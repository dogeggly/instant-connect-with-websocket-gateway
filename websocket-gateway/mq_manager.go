package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"websocket-gateway/pb"

	"github.com/gorilla/websocket"
	amqp "github.com/rabbitmq/amqp091-go"
	"google.golang.org/protobuf/proto"
)

type pushPayload struct {
	Type      int32           `json:"type"`
	MsgId     string          `json:"msgId"`
	SenderId  string          `json:"senderId"`
	GroupId   string          `json:"groupId"`
	Content   string          `json:"content"`
	ExtraData json.RawMessage `json:"extraData"`
}

const (
	directExchangeName = "im.direct.exchange"
	fanoutExchangeName = "im.fanout.exchange"
	queuePrefix        = "ws.queue.node"
	mqAddr             = "192.168.100.131:5672"
	mqUsername         = "dogeggly"
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

	// 3. 声明交换机
	// 参数：name, kind, durable, autoDelete, internal, noWait, args
	err = ch.ExchangeDeclare(
		directExchangeName,
		"direct",
		true,  // durable: 必须为 true
		false, // autoDelete: 必须为 false
		false,
		false,
		nil,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	err = ch.ExchangeDeclare(fanoutExchangeName, "fanout", true, false, false, false, nil)
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
		directExchangeName,
		false,
		nil,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	err = ch.QueueBind(queueName, "", fanoutExchangeName, false, nil)
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
			go handlePushPayload(d)
		}
	}
}

func handlePushPayload(d amqp.Delivery) {
	var mqpl pb.MqPayload
	err := proto.Unmarshal(d.Body, &mqpl)
	if err != nil {
		log.Printf("解析 MQ 消息失败: %v", err)
		// 解析失败属于死信，直接拒绝并不再重试
		_ = d.Reject(false)
		return
	}

	if d.Exchange == fanoutExchangeName {
		localUserIds := make([]int64, 0, len(mqpl.UserIds))
		for _, uid := range mqpl.UserIds {
			userId := strconv.FormatInt(uid, 10)
			if _, exists := cm.get(userId); exists {
				localUserIds = append(localUserIds, uid)
			}
		}

		if len(localUserIds) == 0 {
			_ = d.Ack(false)
			return
		}

		mqpl.UserIds = localUserIds
	}

	switch mqpl.Type {
	case pb.EventType_CHAT_MSG: // 处理普通聊天下发
		for _, uid := range mqpl.UserIds {
			handleChatContent(uid, &mqpl)
		}
		_ = d.Ack(false)
	case pb.EventType_SYS_KICK_OUT: // 处理踢设备下线
		handleKickContent(&mqpl)
		_ = d.Ack(false)
	default:
		log.Printf("未知的指令类型: %d", mqpl.Type)
		_ = d.Reject(false)
	}
}

func handleChatContent(uid int64, mqpl *pb.MqPayload) {
	userId := strconv.FormatInt(uid, 10)
	clientMap, exists := cm.get(userId)
	if !exists {
		log.Printf("目标用户不在当前网关节点，userId=%s", userId)
		return
	}

	ppl := pushPayload{
		Type:      int32(mqpl.Type.Number()),
		MsgId:     strconv.FormatInt(int64(mqpl.MsgId), 10), // 转成字符串发给前端，避免 JS 精度问题
		SenderId:  strconv.FormatInt(mqpl.SenderId, 10),
		GroupId:   strconv.FormatInt(mqpl.GroupId, 10),
		Content:   string(mqpl.Content),
		ExtraData: mqpl.ExtraData,
	}

	pplBytes, err := json.Marshal(ppl)
	if err != nil {
		log.Printf("MQ 推送数据序列化失败 userId=%s err=%v", userId, err)
		return
	}

	for _, client := range clientMap {
		client.enqueueAndWrite(websocket.TextMessage, pplBytes)
	}

	log.Printf("MQ 推送成功 userId=%s", userId)
}

func handleKickContent(mqpl *pb.MqPayload) {
	userId := strconv.FormatInt(mqpl.UserIds[0], 10)

	content := string(mqpl.Content)

	if content == "" {
		log.Printf("踢设备指令缺少 deviceId userId=%s", userId)
		return
	}

	clientMap, exists := cm.get(userId)
	if !exists {
		log.Printf("目标用户不在当前网关节点，无法踢设备 userId=%s deviceId=%s", userId, content)
		return
	}

	targetClient, exists := clientMap[content]
	if !exists {
		log.Printf("目标设备不在当前网关节点，无法踢设备 userId=%s deviceId=%s", userId, content)
		return
	}

	if err := targetClient.Close(); err != nil {
		log.Printf("踢设备关闭连接失败 userId=%s deviceId=%s err=%v", userId, content, err)
		return
	}

	log.Printf("踢设备成功 userId=%s deviceId=%s", userId, content)
}
