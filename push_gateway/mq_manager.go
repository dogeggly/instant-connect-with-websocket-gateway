package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/gorilla/websocket"
	amqp "github.com/rabbitmq/amqp091-go"
)

type PushPayload struct {
	UserId string `json:"userId"`
	Msg    string `json:"msg"`
}

const (
	exchangeName = "ic.gateway.direct.exchange"
	exchangeType = "direct"
	queuePrefix  = "queue.gateway.node"
	mqAddress    = "192.168.1.100:5672"
	username     = "dogeggly"
)

func StartConsumer() error {
	// 1. 建立与 RabbitMQ 的单路 TCP 连接
	mqURL := fmt.Sprintf("amqp://%s:%s@%s", username, defalutMqPassword, mqAddress)
	conn, err := amqp.Dial(mqURL)
	if err != nil {
		return err
	}

	// 2. 在 TCP 连接上开启一个轻量级的 Channel
	ch, err := conn.Channel()
	if err != nil {
		return err
	}

	// 3. 声明总交换机
	// 参数：name, kind, durable, autoDelete, internal, noWait, args
	err = ch.ExchangeDeclare(
		exchangeName,
		exchangeType,
		true,  // durable: 必须为 true
		false, // autoDelete: 必须为 false
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	// 4. 声明网关节点的【专属排他队列】
	queueName := fmt.Sprintf("%s-%d", queuePrefix, workerID)

	// 参数：name, durable, autoDelete, exclusive, noWait, args
	q, err := ch.QueueDeclare(
		queueName,
		false, // durable: 临时队列，不需要持久化到磁盘
		true,  // autoDelete: true! 网关断开时，MQ 自动删掉这个队列
		true,  // exclusive: true! 排他性，只有当前这个连接能访问
		false,
		nil,
	)
	if err != nil {
		return err
	}

	// 5. 将队列绑定到总交换机上
	// 参数：name, key, exchange, noWait, args
	err = ch.QueueBind(
		q.Name,
		string(workerID), // 路由键就是你的 nodeID
		exchangeName,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	// 6. 开始消费当前节点专属队列
	// 参数：queue, consumer, autoAck, exclusive, noLocal, noWait, args
	deliveries, err := ch.Consume(
		q.Name,
		"",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	// 7. 开启专属 goroutine 持续消费消息
	go func() {
		defer func() {
			_ = ch.Close()
			_ = conn.Close()
		}()

		for d := range deliveries {
			handlePushMessage(d)
		}

		log.Println("MQ 投递通道已关闭，停止消费")
	}()

	return nil
}

// handlePushMessage 处理单条推送逻辑
func handlePushMessage(d amqp.Delivery) {
	var payload PushPayload
	err := json.Unmarshal(d.Body, &payload)
	if err != nil {
		log.Printf("解析 MQ 消息失败: %v", err)
		// 解析失败属于死信，直接拒绝并不再重试
		d.Reject(false)
		return
	}

	log.Printf("收到业务层下发指令 -> userId: %s, msg: %s", payload.UserId, payload.Msg)

	clientMap, exists := manager.Get(payload.UserId)
	if !exists {
		log.Printf("目标用户不在当前网关节点，userId=%s", payload.UserId)
		d.Ack(false)
		return
	}

	for _, client := range clientMap {
		if err = client.WriteMessage(websocket.TextMessage, []byte(payload.Msg)); err != nil {
			log.Printf("MQ 推送到 websocket 失败 userId=%s err=%v", payload.UserId, err)
		}
	}

	d.Ack(true)
}
