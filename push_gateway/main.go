package main

import (
	"errors"
	"log"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/bwmarrin/snowflake"
	"github.com/gorilla/websocket"
)

// 心跳常量定义
const (
	// 允许等待客户端 Pong 响应的最大时间。过了这个时间没收到心跳，就踢掉它。
	pongWait = 60 * time.Second
	// 发送 Ping 心跳包的频率。必须小于 pongWait。
	pingPeriod = 45 * time.Second
	httpPort   = ":8080"
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
func (m *ConnectionManager) Get(userId string, deviceId string) (*Client, bool) {
	m.RLock()
	defer m.RUnlock()
	if m.connections[userId] == nil {
		return nil, false
	}
	client, exist := m.connections[userId][deviceId]
	return client, exist
}

// 实例化一个全局的连接管理器
var manager = NewConnectionManager()

// 实例化一个全局的 Redis 路由管理器
var routeManager *RedisRouteManager

// TODO 这里写死网关地址，实际项目中可能是固定的 IP:Port 或者容器的服务名
const gatewayAddress = "127.0.0.1:8080"

// 实例化一个全局的雪花节点（用于生成连接唯一标识）
var snowflakeNode *snowflake.Node

// TODO 生产环境中，网关地址应该是固定的，不能每次重启都变。这里为了测试方便，写死生成一个。
const defaultNodeId = 1

// 定义 Upgrader
// 通过 HTTP 协议发送一个带有 Upgrade 头的请求，服务端同意后，协议"升级"为 WebSocket。
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// TODO 解决跨域问题。开发阶段为了方便前端连接，直接返回 true 允许所有请求。
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// 处理 WebSocket 连接的核心函数 (每个连上来的客户端都会触发这个函数)
func wsHandler(w http.ResponseWriter, r *http.Request) {
	// 将普通 HTTP 连接升级为全双工的 WebSocket 连接
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("协议升级失败:", err)
		return
	}

	// 从 URL 查询参数中获取 userId，实际项目中可能是从 Cookie 或 JWT 中获取
	userId := r.URL.Query().Get("userId")
	deviceId := r.URL.Query().Get("deviceId")
	platform := r.URL.Query().Get("platform")
	// TODO 测试环境，随机分配 userId（10001~10100），默认分配 deviceId 和 platform，后续为必填
	if userId == "" {
		userId = strconv.Itoa(10001 + rand.IntN(100))
	}
	if deviceId == "" {
		deviceId = "default"
	}
	if platform == "" {
		platform = "default"
	}

	connID := snowflakeNode.Generate().String()
	client := &Client{Conn: conn, ConnID: connID}
	// 草泥马别给我处理 err 的警告了
	defer func() {
		_ = client.Close()
	}()

	// 注册连接到内存管理器
	manager.Add(userId, deviceId, client)
	defer manager.Remove(userId, deviceId, client)

	// 注册路由到 Redis
	if err = routeManager.Register(userId, deviceId, platform, client.ConnID); err != nil {
		log.Printf("Redis 注册失败，拒绝连接 userId=%s deviceId=%s err=%v\n", userId, deviceId, err)
	}
	defer func() {
		if err = routeManager.Unregister(userId, deviceId, platform, client.ConnID); err != nil {
			log.Printf("Redis 注销失败 userId=%s deviceId=%s err=%v\n", userId, deviceId, err)
		}
	}()

	// 设置首次读取的绝对超时时间 (当前时间 + 60秒)
	err = client.SetReadDeadline(time.Now().Add(pongWait))
	if err != nil {
		log.Println("设置读取超时失败:", err)
		return
	}

	// 注册 Pong 处理器：一旦收到客户端的 Pong 心跳响应，就续命 60 秒！
	client.SetPongHandler(func(_ string) error {
		// 调试时开启
		// log.Printf("收到 %s 的心跳 Pong 响应，为其续命...\n", userId)
		err = client.SetReadDeadline(time.Now().Add(pongWait))
		if err != nil {
			log.Printf("续命 websocket 连接失败 userId=%s deviceId=%s err=%v\n", userId, deviceId, err)
			return err
		}
		err = routeManager.KeepAlive(userId, deviceId, platform, client.ConnID)
		if err != nil {
			if errors.Is(err, ErrRouteOwnershipLost) {
				log.Printf("连接被顶掉，准备断开 userId=%s deviceId=%s connId=%s\n", userId, deviceId, client.ConnID)
				_ = client.Close()
				return err
			}
			log.Printf("续命 Redis 失败 userId=%s deviceId=%s err=%v\n", userId, deviceId, err)
			return err
		}
		return nil
	})

	// 开启一个专属的 Goroutine，定时给客户端发 Ping 包
	// TODO 心跳风暴
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			<-ticker.C // 每 45 秒执行一次
			// 发送标准的 Ping 控制帧
			if err := client.WriteMessage(websocket.PingMessage, nil); err != nil {
				return // 如果发心跳失败（说明连接已坏），退出协程
			}
		}
	}()

	// 开启死循环，不断读取和发送消息 (Goroutine 的轻量级体现在这里，死循环不会卡死其他用户)
	for {
		// 阻塞读取客户端发来的消息
		messageType, p, err := client.ReadMessage()
		if err != nil {
			log.Printf("读取用户 %s(%s) 消息失败或客户端主动断开: %v\n", userId, deviceId, err)
			break // 报错了就跳出循环，触发上面的 defer 关闭连接
		}

		// TODO 实际业务不需要这段，websocket 只单向发消息
		if messageType != 1 {
			err = client.WriteMessage(websocket.TextMessage, []byte("目前支持发送文字帧"))
			if err != nil {
				log.Println("写入消息失败:", err)
				break
			}
		}

		log.Printf("收到用户 %s(%s) 消息: %s\n", userId, deviceId, string(p))

		// 原封不动地写回给客户端
		err = client.WriteMessage(messageType, p)
		if err != nil {
			log.Println("写入消息失败:", err)
			break
		}
	}
}

// pushHandler 提供给外部调用的 HTTP 推送接口
// 例如: POST /api/push?userId=dogeggly&msg=hello
func pushHandler(w http.ResponseWriter, r *http.Request) {
	// 解析参数
	userId := r.URL.Query().Get("userId")
	deviceId := r.URL.Query().Get("deviceId")
	platform := r.URL.Query().Get("platform")
	msg := r.URL.Query().Get("msg")
	if userId == "" || msg == "" || deviceId == "" || platform == "" {
		http.Error(w, "参数不完整", http.StatusBadRequest)
		return
	}

	// 从我们的内存管理器中寻找这个用户
	client, exists := manager.Get(userId, deviceId)
	if !exists {
		// 用户不在线（未连在这个网关上）
		http.Error(w, "用户不在线", http.StatusNotFound)
		return
	}

	log.Printf("收到用户 %s(%s) 消息: %s\n", userId, deviceId, msg)

	// 找到了！把消息推送下去
	err := client.WriteMessage(websocket.TextMessage, []byte(msg))
	if err != nil {
		http.Error(w, "推送失败", http.StatusInternalServerError)
		return
	}

	_, err = w.Write([]byte("推送成功!"))
	if err != nil {
		http.Error(w, "推送成功，但响应失败", http.StatusInternalServerError)
	}
}

func main() {
	var err error

	// 初始化机器节点
	snowflakeNode, err = snowflake.NewNode(defaultNodeId)
	if err != nil {
		log.Fatalf("初始化雪花节点失败: %v", err)
	}

	// 初始化 Redis 路由管理器
	routeManager, err = NewRedisRouteManager(gatewayAddress)
	if err != nil {
		log.Fatalf("初始化 Redis 路由管理器失败: %v", err)
	}
	log.Println("成功连接到 Redis，路由管理器已初始化")

	// 定义路由：当请求 /ws 时，交给 wsHandler 处理
	http.HandleFunc("/ws", wsHandler)
	// 注册推送接口
	http.HandleFunc("/api/push", pushHandler)
	// 定义端口
	port := httpPort

	log.Printf("WebSocket 服务端已启动在 ws://localhost%s/ws\n", port)

	// 启动 HTTP 服务
	err = http.ListenAndServe(port, nil)
	if err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}
