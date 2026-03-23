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
	httpPort = ":8080"
)

// Client 封装线程安全的客户端
// 解决 gorilla/websocket 不能并发写入的痛点
type Client struct {
	sync.Mutex // 这把锁只保护这一个连接的写入
	*websocket.Conn
	ConnID string
}

// WriteMessage 安全的写方法
func (c *Client) WriteMessage(messageType int, data []byte) error {
	c.Lock()
	defer c.Unlock()
	return c.Conn.WriteMessage(messageType, data)
}

// ConnectionManager 管理所有在线的 WebSocket 连接
type ConnectionManager struct {
	// RWMutex 读写锁。
	// 为什么不用普通的 Mutex(互斥锁)？
	// 因为网关的大部分操作是“查连接发消息”(读操作)，很少是“上线/下线”(写操作)。
	// RWMutex 允许多个协程同时读，大幅提升并发性能。
	sync.RWMutex
	connections map[string]map[string]*Client // 内存中的连接表，格式是 "userId:deviceId" -> Client
}

// NewConnectionManager 初始化管理器
func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[string]map[string]*Client),
	}
}

// Add 添加/注册新连接 (写操作，用完全锁 Lock)
func (m *ConnectionManager) Add(userId string, deviceId string, client *Client) {
	m.Lock()
	defer m.Unlock()
	if m.connections[userId] == nil {
		m.connections[userId] = make(map[string]*Client)
	}
	if oldClient, exists := m.connections[userId][deviceId]; exists {
		// 发现本地已经有同一个设备了！
		// 物理斩断旧连接的 Socket！
		// 这会瞬间引发 oldClient 后台的 ReadMessage 抛出 error，
		// 从而触发旧协程跳出死循环并完美退出，彻底释放资源。
		_ = oldClient.Close()
	}
	m.connections[userId][deviceId] = client
	host := client.RemoteAddr().String()
	log.Printf("[上线] 用户 %s(%s) 已连接，ip 和端口号: %s\n", userId, deviceId, host)
}

// Remove 移除断开的连接 (写操作，用完全锁 Lock)
func (m *ConnectionManager) Remove(userId string, deviceId string, client *Client) {
	m.Lock()
	defer m.Unlock()
	if devices, exists := m.connections[userId]; exists {
		if currentClient, exists := devices[deviceId]; exists {
			// 只有当 Map 里当前的连接，和准备退出的连接是同一个内存地址时，才允许删除！
			if currentClient == client {
				delete(devices, deviceId)
				host := client.RemoteAddr().String()
				log.Printf("[下线] 用户 %s(%s) 已断开，历史 ip 和端口号: %s\n", userId, deviceId, host)
				// 如果这个用户的所有设备都下线了，把外层 Map 的 Key 也清理掉，防止内存泄漏
				if len(devices) == 0 {
					delete(m.connections, userId)
				}
			}
			// 如果指针不同，说明自己已经被“顶号”了，对不碰现有的 Map 数据！
		}
	}
}

// Get 获取指定用户的连接 (读操作，用读锁 RLock，只有当所有读锁释放后，才能上写锁)
func (m *ConnectionManager) Get(userId string) (map[string]*Client, bool) {
	m.RLock()
	defer m.RUnlock()
	if m.connections[userId] == nil {
		return nil, false
	}
	clientMap, exist := m.connections[userId]
	return clientMap, exist
}

// 实例化一个全局的连接管理器
var manager = NewConnectionManager()

// 实例化一个全局的 Redis 路由管理器
var routeManager *RedisRouteManager

// 实例化一个全局的雪花节点（用于生成连接唯一标识）
var snowflakeNode *snowflake.Node

func main() {
	var err error
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 初始化 Redis 路由管理器
	routeManager, err = NewRedisRouteManager()
	if err != nil {
		log.Fatalf("初始化 Redis 路由管理器失败: %v", err)
	}
	// 初始化成功后，启动心跳续命聚合器
	go routeManager.keepAliveAggregatorLoop(ctx)
	log.Println("成功连接到 Redis，路由管理器已初始化")

	// 初始化雪花节点
	snowflakeNode, err = InitSnowflakeNode(routeManager.client)
	if err != nil {
		log.Fatalf("初始化雪花节点失败: %v", err)
	}
	// 初始化成功后，启动雪花节点心跳续命器
	go startSnowflakeWorkerHeartbeat(ctx, routeManager.client)
	log.Println("雪花节点已初始化，WorkerID:", workerID)

	// 启动 MQ 消息消费者
	err = StartConsumer()
	if err != nil {
		log.Fatalf("启动 MQ 消息消费者失败: %v", err)
	}
	log.Println("MQ 消息消费者已启动，等待接收业务层消息...")

	// 定义路由：当请求 /ws 时，交给 wsHandler 处理
	http.HandleFunc("/ws", wsHandler)
	// 注册推送接口
	http.HandleFunc("/api/push", pushHandler)

	log.Printf("WebSocket 服务端已启动在 ws://localhost%s/ws\n", httpPort)

	// 启动 HTTP 服务
	err = http.ListenAndServe(httpPort, nil)
	if err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}
