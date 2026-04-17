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
    // im:upload:{fileHash}, key = status|uploadId|objectKey
    private static final String BUCKET_NAME = "im-files";

    @GetMapping("/init")
    public Result<UploadFileVO> initUpload(String fileHash, String fileName) {

        // 【第一道拦截：秒传判断】
        FileRecord file = fileRecordService.lambdaQuery()
                .eq(FileRecord::getFileHash, fileHash).one();

        if (file != null) {
            UploadFileVO uploadFileVO = UploadFileVO.builder()
                    .status(3).objectKey(file.getObjectKey()).build();
            return Result.success(uploadFileVO);
        }

        // 【第二道拦截：断点续传判断】
        String redisKey = UPLOAD_KEY_PREFIX + fileHash;
        DefaultRedisScript<String> initUploadScript = new DefaultRedisScript<>();
        initUploadScript.setScriptText(
                "local value = redis.call('GET', KEYS[1]) " +
                        "if not value then " +
                        "  redis.call('SET', KEYS[1], ARGV[1], 'EX', tonumber(ARGV[2])) " +
                        "  return '0'" +
                        "end " +
                        "return value"
        );
        initUploadScript.setResultType(String.class);

        String scriptResult = redisTemplate.execute(initUploadScript, List.of(redisKey), "1||", "10");

        if (!scriptResult.equals("0")) {
            String[] v = scriptResult.split("\\|");
            String status = v[0];
            String uploadId = v[1];
            String objectKey = v[2];

            if ("1".equals(status)) {
                // 说明别人正在建任务
                return Result.success(UploadFileVO.builder().status(1).build());
            }
            if ("3".equals(status)) {
                // 说明文件已经合并完成了，直接返回成功让前端秒传
                return Result.success(UploadFileVO.builder().status(3).objectKey(objectKey).build());
            }

            // 去 MinIO 查询已经传了哪些分片
            ListPartsResponse listPartsResponse = s3Client.listParts(b -> b
                    .bucket(BUCKET_NAME)
                    .key(objectKey)
                    .uploadId(uploadId)
            );

            // 提取已完成的分片号和 eTag 返回给前端，后续前端计算还未完成的分片号去申请 url
            List<UploadFilePartInfo> uploadFilePartInfoList = listPartsResponse.parts().stream()
                    .map(part -> UploadFilePartInfo.builder()
                            .partNumber(part.partNumber()).eTag(part.eTag()).build())
                    .toList();

            // 需要前端判断一下回传的 partNumber 是否为空
            UploadFileVO uploadFileVO = UploadFileVO.builder()
                    .status(2).objectKey(objectKey).uploadId(uploadId).uploadFilePartInfoList(uploadFilePartInfoList).build();
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
    public Result<UploadFileVO> completeUpload(@RequestBody FileRecord fileRecord,
                                               String uploadId, List<UploadFilePartInfo> list) {

        // 1. 将前端传来的 ETag 列表转换成 AWS SDK 需要的对象
        List<CompletedPart> completedParts = list.stream()
                .map(p -> CompletedPart.builder()
                        .partNumber(p.getPartNumber())
                        .eTag(p.getETag())
                        .build())
                .toList();

        CompletedMultipartUpload completedUpload = CompletedMultipartUpload.builder()
                .parts(completedParts)
                .build();

        // 2. 告诉 MinIO 进行文件合并
        try {
            s3Client.completeMultipartUpload(b -> b
                    .bucket(BUCKET_NAME)
                    .key(fileRecord.getObjectKey())
                    .uploadId(uploadId)
                    .multipartUpload(completedUpload));
        } catch (NoSuchUploadException e) {
            // 走到这里，说明 UploadId 不存在了（可能刚才自己重试合并过，或者被别的并发线程合并了）
            // 头铁去 MinIO 查一下，这文件到底在不在？
            s3Client.headObject(b -> b.bucket(BUCKET_NAME).key(fileRecord.getObjectKey()));
            // 如果没报错，说明文件已经稳稳躺在里面了，把异常吞掉，当作合并成功继续往下走！
        }

        // 3. 合并成功，将元数据落库 (带防并发冲突兜底)
        fileRecord.setFileId(snowflake.nextId());
        fileRecordService.saveFile(fileRecord);

        // 4. 流转 Redis 里的临时任务的状态 (墓碑机制)
        String redisKey = UPLOAD_KEY_PREFIX + fileRecord.getFileHash();
        redisTemplate.opsForValue().set(redisKey, "3||" + fileRecord.getObjectKey(), Duration.ofMinutes(1));

        return Result.success(UploadFileVO.builder().status(3).objectKey(fileRecord.getObjectKey()).build());
    }

}
