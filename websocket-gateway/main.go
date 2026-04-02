package main

import (
	"context"
	"log"
	"net/http"

	"github.com/bwmarrin/snowflake"
)

const (
	httpPort = ":8082"
	// 这里写死。
	fixedNodeID = 1
)

// 实例化一个全局的连接管理器
var cm *connectionManager

// 实例化一个全局的 Redis 路由管理器
var rm *redisManager

// 实例化一个全局的网关节点
var nodeId int64

// 实例化一个雪花节点
var snowflakeNode *snowflake.Node

// 实例化一个时间轮
var tw *timeWheel

func main() {
	var err error
	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer close(keepAliveChannel)

	// 初始化一个全局的连接管理器
	cm = newConnectionManager()

	// 初始化 Redis 路由管理器
	rm, err = newRedisManager()
	defer func() {
		_ = rm.Close()
	}()
	if err != nil {
		log.Fatalf("初始化 Redis 路由管理器失败: %v", err)
	}
	go func() {
		if err = rm.keepAliveAggregator(ctx); err != nil {
			errCh <- err
		}
	}()
	log.Println("成功连接到 Redis，路由管理器已初始化")

	// 初始化网关节点
	var lockOwner string
	nodeId, lockOwner, err = initNodeId(rm)
	if err != nil {
		log.Fatalf("初始化网关节点管理器失败: %v", err)
	}
	go func() {
		if err = rm.startNodeHeartbeat(ctx, lockOwner); err != nil {
			errCh <- err
		}
	}()
	log.Println("网关节点已初始化，nodeId:", nodeId)

	// 初始化雪花节点
	snowflakeNode, err = snowflake.NewNode((fixedNodeID << 5) | nodeId)
	if err != nil {
		log.Fatalf("初始化雪花节点失败: %v", err)
	}
	log.Println("雪花节点已初始化")

	// 初始化 MQ 消息消费者
	conn, ch, deliveries, err := initMqConsumer()
	defer func() {
		_ = ch.Close()
		_ = conn.Close()
	}()
	if err != nil {
		log.Fatalf("初始化 MQ 消息消费者失败: %v", err)
	}
	go func() {
		if err = startMqConsumer(ctx, deliveries); err != nil {
			errCh <- err
		}
	}()
	log.Println("MQ 消息消费者已启动，等待接收业务层消息...")

	// 初始化时间轮
	tw = initTimeWheel()
	go func() {
		if err = tw.startTimeWheel(ctx); err != nil {
			errCh <- err
		}
	}()
	log.Println("时间轮已启动")

	// 定义路由：当请求 /ws 时，交给 wsHandler 处理
	http.HandleFunc("/ws", wsHandler)
	// 注册推送接口，调试时开启
	// http.HandleFunc("/api/push", pushHandler)

	log.Printf("WebSocket 服务端已启动在 ws://localhost%s/ws\n", httpPort)

	// 启动 HTTP 服务
	go func() {
		if err = http.ListenAndServe(httpPort, nil); err != nil {
			log.Printf("HTTP 服务器发生错误: %v\n", err)
			errCh <- err
		}
	}()

	// 等待错误发生，一旦发生就退出主函数，优雅关闭所有资源
	for {
		err = <-errCh
		if err != nil {
			log.Printf("服务发生错误: %v\n", err)
			return
		}
	}
}
