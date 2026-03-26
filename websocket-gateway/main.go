package main

import (
	"context"
	"log"
	"net/http"
	"sync"

	"github.com/bwmarrin/snowflake"
	"github.com/gorilla/websocket"
)

const (
	httpPort = ":8082"
	// 这里写死。
	fixedNodeID = 1
)

// client 封装线程安全的客户端
// 解决 gorilla/websocket 不能并发写入的痛点
type client struct {
	sync.Mutex // 这把锁只保护这一个连接的写入
	*websocket.Conn
	connID string
}

// writeMessage 安全的写方法
func (c *client) writeMessage(messageType int, data []byte) error {
	c.Lock()
	defer c.Unlock()
	return c.Conn.WriteMessage(messageType, data)
}

// connectionManager 管理所有在线的 WebSocket 连接
type connectionManager struct {
	// RWMutex 读写锁。
	// 为什么不用普通的 Mutex(互斥锁)？
	// 因为网关的大部分操作是“查连接发消息”(读操作)，很少是“上线/下线”(写操作)。
	// RWMutex 允许多个协程同时读，大幅提升并发性能。
	sync.RWMutex
	connections map[string]map[string]*client // 内存中的连接表，格式是 "userId:deviceId" -> *client
}

// newConnectionManager 初始化管理器
func newConnectionManager() *connectionManager {
	return &connectionManager{
		connections: make(map[string]map[string]*client),
	}
}

// add 添加/注册新连接 (写操作，用完全锁 Lock)
func (cm *connectionManager) add(userId string, deviceId string, c *client) {
	cm.Lock()
	defer cm.Unlock()
	if cm.connections[userId] == nil {
		cm.connections[userId] = make(map[string]*client)
	}
	if oldClient, exists := cm.connections[userId][deviceId]; exists {
		// 发现本地已经有同一个设备了！
		// 物理斩断旧连接的 Socket！
		// 这会瞬间引发 oldClient 后台的 ReadMessage 抛出 error，
		// 从而触发旧协程跳出死循环并完美退出，彻底释放资源。
		_ = oldClient.Close()
	}
	cm.connections[userId][deviceId] = c
	host := c.RemoteAddr().String()
	log.Printf("[上线] 用户 %s(%s) 已连接，ip 和端口号: %s\n", userId, deviceId, host)
}

// remove 移除断开的连接 (写操作，用完全锁 Lock)
func (cm *connectionManager) remove(userId string, deviceId string, c *client) {
	cm.Lock()
	defer cm.Unlock()
	if devices, exists := cm.connections[userId]; exists {
		if currentClient, exists := devices[deviceId]; exists {
			// 只有当 Map 里当前的连接，和准备退出的连接是同一个内存地址时，才允许删除！
			if currentClient == c {
				delete(devices, deviceId)
				host := c.RemoteAddr().String()
				log.Printf("[下线] 用户 %s(%s) 已断开，历史 ip 和端口号: %s\n", userId, deviceId, host)
				// 如果这个用户的所有设备都下线了，把外层 Map 的 Key 也清理掉，防止内存泄漏
				if len(devices) == 0 {
					delete(cm.connections, userId)
				}
			}
			// 如果指针不同，说明自己已经被“顶号”了，对不碰现有的 Map 数据！
		}
	}
}

// get 获取指定用户的连接 (读操作，用读锁 RLock，只有当所有读锁释放后，才能上写锁)
func (cm *connectionManager) get(userId string) (map[string]*client, bool) {
	cm.RLock()
	defer cm.RUnlock()
	if cm.connections[userId] == nil {
		return nil, false
	}
	clientMap, exist := cm.connections[userId]
	return clientMap, exist
}

// 实例并初始化一个全局的连接管理器
var cm = newConnectionManager()

// 实例化一个全局的 Redis 路由管理器
var rm *redisManager

// 实例化一个全局的网关节点
var nodeId int64

// 实例化一个雪花节点
var snowflakeNode *snowflake.Node

func main() {
	var err error
	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer close(keepAliveChannel)

	// 初始化 Redis 路由管理器
	rm, err = newRedisManager()
	defer func() {
		_ = rm.Close()
	}()
	if err != nil {
		log.Fatalf("初始化 Redis 路由管理器失败: %v", err)
	}
	// 初始化成功后，启动心跳续命聚合器
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
	// 初始化成功后，启动网关节点心跳续命器
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
