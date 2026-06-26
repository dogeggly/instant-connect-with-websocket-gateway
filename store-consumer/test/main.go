package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"loadtest/pb"

	amqp "github.com/rabbitmq/amqp091-go"
	"google.golang.org/protobuf/proto"
)

const (
	exchange   = "im.direct.store.exchange"
	routingKey = "store"
	queue      = "im.store.queue"
)

var (
	rabbitUser = "dogeggly"
	rabbitPass = "512218"
	rabbitHost = "192.168.100.131"
	rabbitPort = 5672
)

func main() {
	scenario := flag.String("scenario", "single", "测试场景: single, group10, group100")
	count := flag.Int("count", 5000, "发送消息数量")
	purge := flag.Bool("purge", true, "发送前是否清空队列")
	flag.Parse()

	if *scenario != "single" && *scenario != "group10" && *scenario != "group100" {
		log.Fatalf("无效场景: %s, 可选: single, group10, group100", *scenario)
	}

	url := fmt.Sprintf("amqp://%s:%s@%s:%d/", rabbitUser, rabbitPass, rabbitHost, rabbitPort)
	conn, err := amqp.Dial(url)
	if err != nil {
		log.Fatalf("连接 RabbitMQ 失败: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("创建 Channel 失败: %v", err)
	}
	defer ch.Close()

	// 清空队列，保证干净的测量基线
	if *purge {
		purged, err := ch.QueuePurge(queue, false)
		if err != nil {
			log.Fatalf("清空队列失败: %v", err)
		}
		log.Printf("已清空队列，移除了 %d 条旧消息", purged)
	}

	// 启用发布确认
	if err := ch.Confirm(false); err != nil {
		log.Fatalf("启用发布确认失败: %v", err)
	}
	confirms := ch.NotifyPublish(make(chan amqp.Confirmation, 10000))

	rowsPerMsg := timelineRowsPerMsg(*scenario)
	totalRows := *count * rowsPerMsg

	fmt.Println()
	fmt.Printf("========== 场景: %s ==========\n", *scenario)
	fmt.Printf("消息数量:   %d\n", *count)
	fmt.Printf("每消息行数: %d\n", rowsPerMsg)
	fmt.Printf("预计总行数: %d\n", totalRows)
	fmt.Println()

	log.Println("开始发送...")
	pubStartTime := time.Now()

	switch *scenario {
	case "single":
		publishSingleChat(ch, *count)
	case "group10":
		publishGroupChat(ch, *count, 50001, 10)
	case "group100":
		publishGroupChat(ch, *count, 50002, 100)
	}

	// 等待所有发布确认
	acked := 0
	for acked < *count {
		confirm := <-confirms
		if confirm.Ack {
			acked++
		} else {
			log.Printf("消息 %d 未确认", confirm.DeliveryTag)
		}
	}

	pubEndTime := time.Now()
	pubElapsed := pubEndTime.Sub(pubStartTime)
	pubRate := float64(*count) / pubElapsed.Seconds()

	fmt.Println()
	fmt.Printf("发布完成，耗时: %v，速率: %.0f msg/s\n", pubElapsed.Round(time.Millisecond), pubRate)
	fmt.Println()

	// ========== 监控队列排空，计算消费者 TPS ==========
	log.Println("开始监控队列消费进度（50ms 采样，每秒打印一次）...")
	log.Println("(如果消费者未启动，队列不会减少，60 秒后自动超时)")
	fmt.Println()

	drainStart := time.Now()
	prevReportDepth := 0
	prevReportTime := drainStart
	lastNonZeroTime := drainStart
	lastNonZeroDepth := *count
	deadline := time.Now().Add(60 * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		q, err := ch.QueueInspect(queue)
		if err != nil {
			log.Printf("查询队列失败: %v", err)
			continue
		}
		currentDepth := q.Messages
		now := time.Now()

		if currentDepth > 0 {
			lastNonZeroTime = now
			lastNonZeroDepth = currentDepth
		}

		// 每秒打印一次进度
		if now.Sub(prevReportTime) >= time.Second {
			if prevReportDepth > 0 && currentDepth != prevReportDepth {
				consumed := prevReportDepth - currentDepth
				log.Printf("  [%6s] 队列剩余 %5d 条 | 本秒消费 %4d 条",
					now.Sub(drainStart).Round(time.Second), currentDepth, consumed)
			}
			prevReportTime = now
			prevReportDepth = currentDepth
		}

		if currentDepth == 0 {
			break
		}

		if now.After(deadline) {
			log.Println("!!! 消费者似乎未运行或处理太慢，超时退出监控")
			log.Println("!!! 请检查 store-consumer 是否启动")
			os.Exit(1)
		}

		<-ticker.C
	}

	// 用最后一条非零记录做插值，避免末尾轮询误差
	// 如果最后观测到 lastNonZeroDepth 条在 lastNonZeroTime，且之后队列变空，
	// 用最近 1 秒的平均消费速率估算实际排空时间
	elapsedSinceLastNonZero := time.Since(lastNonZeroTime)
	var drainEnd time.Time
	if lastNonZeroDepth > 0 && elapsedSinceLastNonZero > 0 {
		// 最近 1 秒内的消费速率
		recentRate := float64(lastNonZeroDepth) / elapsedSinceLastNonZero.Seconds()
		if recentRate > 0 {
			// 按此速率，最后一批消息应该在 ~0.5 个采样周期内被消费
			estimatedRemaining := time.Duration(float64(lastNonZeroDepth)/recentRate) * time.Second
			if estimatedRemaining < elapsedSinceLastNonZero {
				drainEnd = lastNonZeroTime.Add(estimatedRemaining)
			} else {
				drainEnd = time.Now()
			}
		} else {
			drainEnd = time.Now()
		}
	} else {
		drainEnd = time.Now()
	}
	drainElapsed := drainEnd.Sub(drainStart)
	consumerMsgRate := float64(*count) / drainElapsed.Seconds()
	consumerRowRate := float64(totalRows) / drainElapsed.Seconds()

	fmt.Println()
	fmt.Println("╔══════════════════════════════════╗")
	fmt.Println("║         测 试 结 果             ║")
	fmt.Println("╠══════════════════════════════════╣")
	fmt.Printf("║ 场景:        %-20s║\n", *scenario)
	fmt.Printf("║ 消息总数:    %-5d               ║\n", *count)
	fmt.Printf("║ 每消息行数:  %-5d               ║\n", rowsPerMsg)
	fmt.Printf("║ 总写入行数:  %-5d               ║\n", totalRows)
	fmt.Println("╠══════════════════════════════════╣")
	fmt.Printf("║ 发布耗时:    %-14s       ║\n", pubElapsed.Round(time.Millisecond))
	fmt.Printf("║ 发布速率:    %-7.0f msg/s       ║\n", pubRate)
	fmt.Println("╠══════════════════════════════════╣")
	fmt.Printf("║ 消费耗时:    %-14s       ║\n", drainElapsed.Round(time.Millisecond))
	fmt.Printf("║ 消费速率:    %-7.0f msg/s       ║\n", consumerMsgRate)
	fmt.Printf("║ 落库 TPS:    %-7.0f 行/s       ║\n", consumerRowRate)
	fmt.Println("╚══════════════════════════════════╝")
}

func timelineRowsPerMsg(scenario string) int {
	switch scenario {
	case "single":
		return 2 // sender + receiver
	case "group10":
		return 10
	case "group100":
		return 100
	default:
		return 0
	}
}

// publishSingleChat 发送单聊消息
// 使用轮转的 userId 对，避免 seq_id 热点集中在少数用户上
func publishSingleChat(ch *amqp.Channel, count int) {
	baseID := int64(40001)
	for i := 0; i < count; i++ {
		senderID := baseID + int64(i*2)
		receiverID := baseID + int64(i*2+1)
		msgID := uint64(6000000000000000000 + i)

		payload := &pb.MqStorePayload{
			MsgId:      msgID,
			SenderId:   senderID,
			ReceiverId: receiverID,
			IsGroup:    false,
		}
		publish(ch, payload)
	}
}

// publishGroupChat 发送群聊消息
func publishGroupChat(ch *amqp.Channel, count int, groupID int64, memberCount int) {
	_ = memberCount // 成员数由消费者从 Redis 读取，这里仅标记
	for i := 0; i < count; i++ {
		msgID := uint64(6000000000000000000 + i)
		senderID := int64(40001)

		payload := &pb.MqStorePayload{
			MsgId:      msgID,
			SenderId:   senderID,
			ReceiverId: groupID,
			IsGroup:    true,
		}
		publish(ch, payload)
	}
}

func publish(ch *amqp.Channel, payload *pb.MqStorePayload) {
	body, err := proto.Marshal(payload)
	if err != nil {
		log.Printf("序列化失败: %v", err)
		os.Exit(1)
	}

	err = ch.Publish(
		exchange,
		routingKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType:  "application/x-protobuf",
			DeliveryMode: amqp.Persistent,
			Body:         body,
		},
	)
	if err != nil {
		log.Printf("发布失败: %v", err)
		os.Exit(1)
	}
}
