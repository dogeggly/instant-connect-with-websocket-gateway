package com.dely.im.controller;

import cn.hutool.core.lang.Snowflake;
import com.dely.im.entity.FileRecord;
import com.dely.im.service.IFileRecordService;
import com.dely.im.utils.UploadFilePartInfo;
import com.dely.im.utils.UploadFileVO;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.data.redis.core.script.DefaultRedisScript;
import org.springframework.web.bind.annotation.RequestMapping;

import org.springframework.web.bind.annotation.RestController;

import com.dely.im.utils.Result;
import org.springframework.web.bind.annotation.*;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.model.*;
import software.amazon.awssdk.services.s3.presigner.S3Presigner;
import software.amazon.awssdk.services.s3.presigner.model.PresignedUploadPartRequest;
import software.amazon.awssdk.services.s3.presigner.model.UploadPartPresignRequest;

import java.time.Duration;
import java.time.LocalTime;
import java.time.format.DateTimeFormatter;
import java.util.*;

/**
 * <p>
 * 前端控制器
 * </p>
 *
 * @author dely
 * @since 2026-04-06
 */
@RestController
@RequestMapping("/file")
public class FileRecordController {

    @Autowired
    private IFileRecordService fileRecordService;

    @Autowired
    private StringRedisTemplate redisTemplate;

    @Autowired
    private S3Client s3Client;

    @Autowired
    private S3Presigner s3Presigner;

    @Autowired
    private Snowflake snowflake;

    private static final String UPLOAD_KEY_PREFIX = "im:upload:";
    private static final String BUCKET_NAME = "im-files";

    @GetMapping("/init")
    public Result<UploadFileVO> initUpload(String fileHash, String fileName) {

        // 【第一道拦截：秒传判断】
        FileRecord file = fileRecordService.lambdaQuery()
                .eq(FileRecord::getFileHash, fileHash).one();

        if (file != null) {
            UploadFileVO uploadFileVO = UploadFileVO.builder()
                    .status(4).objectKey(file.getObjectKey()).build();
            return Result.success(uploadFileVO);
        }

        // 【第二道拦截：断点续传判断】
        String redisKey = UPLOAD_KEY_PREFIX + fileHash;
        Boolean isFirst = redisTemplate.opsForValue().setIfAbsent(redisKey, "1||", Duration.ofSeconds(5));

        if (Boolean.FALSE.equals(isFirst)) {
            String value = redisTemplate.opsForValue().get(redisKey);
            String[] v = value.split("\\|");
            String status = v[0];
            String uploadId = v[1];
            String objectKey = v[2];

            if ("1".equals(status)) {
                // 说明别人正在建任务
                return Result.success(UploadFileVO.builder().status(1).build());
            }
            if ("3".equals(status)) {
                // 说明文件极速合并中
                return Result.success(UploadFileVO.builder().status(3).build());
            }
            if ("4".equals(status)) {
                // 说明文件已经合并完成了，直接返回成功让前端秒传
                return Result.success(UploadFileVO.builder().status(4).objectKey(objectKey).build());
            }

            // 去 MinIO 查询已经传了哪些分片
            ListPartsResponse listPartsResponse = s3Client.listParts(b -> b
                    .bucket(BUCKET_NAME)
                    .key(objectKey)
                    .uploadId(uploadId)
            );

            // 提取已完成的分片号返回给前端
            List<Integer> partNumber = listPartsResponse.parts().stream()
                    .map(Part::partNumber)
                    .toList();

            // 需要前端判断一下回传的 partNumber 是否为空
            UploadFileVO uploadFileVO = UploadFileVO.builder()
                    .status(2).objectKey(objectKey).uploadId(uploadId).partNumber(partNumber).build();
            return Result.success(uploadFileVO);
        }

        // 【第三步：既没有秒传，也没有续传，创建全新上传任务】
        // 生成文件在 OSS 中的唯一路径
        String time = LocalTime.now().format(DateTimeFormatter.ofPattern("yyyyMMdd"));
        String objectKey = time + fileHash + "_" + fileName;

        CreateMultipartUploadResponse createResponse = s3Client.createMultipartUpload(b -> b
                .bucket(BUCKET_NAME)
                .key(objectKey));

        String newUploadId = createResponse.uploadId();

        // 存入 Redis
        redisTemplate.opsForValue().set(redisKey, "2|" + newUploadId + "|" + objectKey, Duration.ofHours(24));

        UploadFileVO uploadFileVO = UploadFileVO.builder()
                .status(2).objectKey(objectKey).uploadId(newUploadId).build();
        return Result.success(uploadFileVO);
    }

    @GetMapping("/part")
    public Result<List<UploadFilePartInfo>> generatePartUrls(String uploadId, String objectKey, List<Integer> partNumbers) {

        List<UploadFilePartInfo> list = new ArrayList<>();

        // 循环生成每个分片的 PUT 预签名 URL
        for (Integer partNumber : partNumbers) {
            UploadPartRequest uploadPartRequest = UploadPartRequest.builder()
                    .bucket(BUCKET_NAME)
                    .key(objectKey)
                    .uploadId(uploadId)
                    .partNumber(partNumber)
                    .build();

            // 生成有效期为 15 分钟的 URL
            UploadPartPresignRequest presignRequest = UploadPartPresignRequest.builder()
                    .signatureDuration(Duration.ofMinutes(15))
                    .uploadPartRequest(uploadPartRequest)
                    .build();

            PresignedUploadPartRequest presigned = s3Presigner.presignUploadPart(presignRequest);

            UploadFilePartInfo partInfo = UploadFilePartInfo.builder()
                    .partNumber(partNumber).partUrl(presigned.url().toString()).build();
            list.add(partInfo);
        }
        return Result.success(list);
    }

    @PostMapping("/complete")
    public Result<Integer> completeUpload(@RequestBody FileRecord fileRecord,
                                          String uploadId, List<UploadFilePartInfo> list) {

        // 1. 先流转 Redis 里的临时任务的状态
        String redisKey = UPLOAD_KEY_PREFIX + fileRecord.getFileHash();
        String expectedValue = "2|" + uploadId + "|" + fileRecord.getObjectKey();
        DefaultRedisScript<Long> checkUploadScript = new DefaultRedisScript<>();
        checkUploadScript.setScriptText(
                "local value = redis.call('GET', KEYS[1]) " +
                        "if not value then return 0 end " +
                        "if value ~= ARGV[1] then return -1 end " +
                        "return 1"
        );
        checkUploadScript.setResultType(Long.class);

        long checkResult = redisTemplate.execute(checkUploadScript, List.of(redisKey), expectedValue);

        if (checkResult == 0L) {
            // redis key 不存在，ttl 过期
            return Result.success(0);
        }
        if (checkResult == -1L) {
            // redis 值不匹配，被其他线程抢占
            return Result.success(3);
        }

        // 2. 将前端传来的 ETag 列表转换成 AWS SDK 需要的对象
        List<CompletedPart> completedParts = list.stream()
                .map(p -> CompletedPart.builder()
                        .partNumber(p.getPartNumber())
                        .eTag(p.getETag())
                        .build())
                .toList();

        CompletedMultipartUpload completedUpload = CompletedMultipartUpload.builder()
                .parts(completedParts)
                .build();

        // 3. 告诉 MinIO 进行文件合并
        s3Client.completeMultipartUpload(b -> b
                .bucket(BUCKET_NAME)
                .key(fileRecord.getObjectKey())
                .uploadId(uploadId)
                .multipartUpload(completedUpload));

        // 4. 再次流转 Redis 里的临时任务的状态
        redisTemplate.opsForValue().set(redisKey, "4||" + fileRecord.getObjectKey(), Duration.ofSeconds(20));

        // 5. 合并成功，将元数据落库 (秒传就靠它了)
        fileRecord.setFileId(snowflake.nextId());
        fileRecordService.save(fileRecord);

        return Result.success(4);
    }

}
