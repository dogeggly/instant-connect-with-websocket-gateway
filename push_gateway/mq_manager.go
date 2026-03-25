package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"

	"github.com/gorilla/websocket"
	amqp "github.com/rabbitmq/amqp091-go"
)

type pushPayLoad struct {
	UserId  string      `json:"userId"`
	Content pushContent `json:"content"`
}

type pushContent struct {
	Msg string `json:"msg"`
}

const (
	exchangeName = "ic.direct.exchange"
	exchangeType = "direct"
	queuePrefix  = "ws.queue.node"
	mqAddr       = "192.168.100.131:5672"
	mqUsername   = "dogeggly"
)

func initMqConsumer() (*amqp.Connection, *amqp.Channel, <-chan amqp.Delivery, error) {
	// 1. 建立与 RabbitMQ 的单路 TCP 连接
	mqURL := fmt.Sprintf("amqp://%s:%s@%s", mqUsername, mqPassword, mqAddr)
	conn, err := amqp.Dial(mqURL)
	if err != nil {
		return nil, nil, nil, err
	}

	// 2. 在 TCP 连接上开启一个轻量级的 Channel
	ch, err := conn.Channel()
	if err != nil {
		return nil, nil, nil, err
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
		return nil, nil, nil, err
	}

	// 4. 声明网关节点的【专属排他队列】
	queueName := fmt.Sprintf("%s-%d", queuePrefix, nodeId)
	// 参数：name, durable, autoDelete, exclusive, noWait, args
	_, err = ch.QueueDeclare(
		queueName,
		false, // durable: 临时队列，不需要持久化到磁盘
		true,  // autoDelete: true! 网关断开时，MQ 自动删掉这个队列
		true,  // exclusive: true! 排他性，只有当前这个连接能访问
		false,
		nil,
	)
	if err != nil {
		return nil, nil, nil, err
	}

	// 5. 将队列绑定到总交换机上
	// 参数：name, key, exchange, noWait, args
	err = ch.QueueBind(
		queueName,
		strconv.FormatInt(nodeId, 10), // 路由键就是你的 nodeID
		exchangeName,
		false,
		nil,
	)
	if err != nil {
		return nil, nil, nil, err
	}

	// 6. 开始消费当前节点专属队列
	// 参数：queue, consumer, autoAck, exclusive, noLocal, noWait, args
	deliveries, err := ch.Consume(
		queueName,
		strconv.FormatInt(nodeId, 10),
		false, // autoAck: false! 需要手动 ack，确保消息不丢失
		true,
		false,
		false,
		nil,
	)
	if err != nil {
		return nil, nil, nil, err
	}

	return conn, ch, deliveries, nil
}

func startMqConsumer(ctx context.Context, deliveries <-chan amqp.Delivery) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("MQ 消费队列已关闭")
			}
			// 开启一个微线程(Goroutine)去处理每一条消息，绝不阻塞主消费队列
			go handlePushMessage(d)
		}
	}
}

// handlePushMessage 处理单条推送逻辑
func handlePushMessage(d amqp.Delivery) {
	var payload pushPayLoad
	err := json.Unmarshal(d.Body, &payload)
	if err != nil {
		log.Printf("解析 MQ 消息失败: %v", err)
		// 解析失败属于死信，直接拒绝并不再重试
		d.Reject(false)
		return
	}

	log.Printf("收到业务层下发指令 -> userId: %s, msg: %s", payload.UserId, payload.Content.Msg)

	clientMap, exists := cm.get(payload.UserId)
	if !exists {
		log.Printf("目标用户不在当前网关节点，userId=%s", payload.UserId)
		d.Ack(false)
		return
	}

	for _, client := range clientMap {
		if err = client.writeMessage(websocket.TextMessage, []byte(payload.Content.Msg)); err != nil {
			log.Printf("MQ 推送到 websocket 失败 userId=%s err=%v", payload.UserId, err)
		}
	}

	d.Ack(false)
}
