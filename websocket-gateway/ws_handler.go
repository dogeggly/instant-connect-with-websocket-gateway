package main

import (
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	// buffer 复用池的上限，超过这个容量的数组不回收，防止内存囤积
	maxPooledBufferCap = 128
)

// bufferPool 跨连接复用 []packet 的底层数组，减少连接变动带来的 GC 压力
var bufferPool = sync.Pool{
	New: func() any {
		return make([]packet, 0, 8)
	},
}

type packet struct {
	msgType int    // websocket.TextMessage 或者是 websocket.PingMessage
	data    []byte // 具体的 payload
}

// client 封装线程安全的客户端
// 解决 gorilla/websocket 不能并发写入的痛点
type client struct {
	sync.Mutex // 这把锁只保护这一个连接的写入
	*websocket.Conn
	connID        string
	userId        string
	deviceId      string
	platform      string
	isClose       atomic.Bool  // 用于时钟轮判断，默认初始化为 false，不用上面的锁轻量一些
	lastHeartbeat atomic.Int64 // 其实这里理应不存在并发读写，但还是保险起见上个 atomic
	buffer        []packet
	isWriting     atomic.Bool
}

func (c *client) enqueueAndWrite(msgType int, data []byte) {
	c.Lock()
	// 1. buffer 为 nil 时从池子取，跨连接复用底层数组
	if c.buffer == nil {
		c.buffer = bufferPool.Get().([]packet)[:0]
	}
	c.buffer = append(c.buffer, packet{msgType, data})
	c.Unlock()

	// 2. 如果当前没有人在发包，我就启动一个临时工去发！
	if c.isWriting.CompareAndSwap(false, true) {
		go c.flushBuffer() // 起飞！
	}
}

// 专门负责清空 buffer 的临时工协程（发完就销毁，绝不常驻待命！）
func (c *client) flushBuffer() {
	for {
		c.Lock()
		if len(c.buffer) == 0 {
			c.isWriting.Store(false)
			c.Unlock()
			return
		}

		batch := c.buffer
		c.buffer = nil
		c.Unlock()

		for _, msg := range batch {
			if err := c.WriteMessage(msg.msgType, msg.data); err != nil {
				_ = c.Close()
				return
			}
		}
		// 写完归还底层数组到池子，跨连接复用，降低 GC 压力
		if cap(batch) <= maxPooledBufferCap {
			clear(batch) // 清掉 data 引用，避免内存泄漏
			bufferPool.Put(batch[:0])
		}
	}
}

// 定义 Upgrader
// 通过 HTTP 协议发送一个带有 Upgrade 头的请求，服务端同意后，协议"升级"为 WebSocket。
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// 解决跨域问题。开发阶段为了方便前端连接，直接返回 true 允许所有请求。
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// 处理 WebSocket 连接的核心函数 (每个连上来的客户端都会触发这个函数)
func wsHandler(w http.ResponseWriter, r *http.Request) {
	// 测试环境，从 URL 查询参数中获取 userId，或随机分配 userId（10001~10100），实际项目中从 JWT 中获取
	userId := r.URL.Query().Get("userId")
	if userId == "" {
		userId = strconv.Itoa(10001 + rand.IntN(100))
	}

	deviceId := r.URL.Query().Get("deviceId")
	platform := r.URL.Query().Get("platform")
	// 测试环境，默认分配 deviceId 和 platform，后续为前端必填
	if deviceId == "" {
		deviceId = "default"
	}
	if platform == "" {
		platform = "default"
	}

	/*	// 从 URL 参数中只获取 Token，不获取 userId 了
		tokenString := r.URL.Query().Get("token")
		if tokenString == "" {
			http.Error(w, "token 校验失败", http.StatusUnauthorized)
			return
		}

		// 本地 CPU 极速校验 JWT 并解析出 userId
		userId, err := validateTokenAndGetUserId(tokenString)
		if err != nil {
			http.Error(w, "token 校验失败", http.StatusUnauthorized)
			return
		}*/

	// 将普通 HTTP 连接升级为全双工的 WebSocket 连接
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("协议升级失败:", err)
		return
	}

	connID := uuid.NewString()
	c := &client{
		Conn:     conn,
		connID:   connID,
		userId:   userId,
		deviceId: deviceId,
		platform: platform,
	}
	c.lastHeartbeat.Store(time.Now().Unix())
	defer func() {
		c.isClose.Store(true) // 标记连接已关闭，时钟轮会检查这个标记来决定是否续命
		_ = c.Close()
	}()

	// 注册连接到内存管理器
	cm.add(userId, deviceId, c)
	defer cm.remove(userId, deviceId, c)

	// 注册路由到 Redis
	if err = rm.register(userId, deviceId, platform, c.connID); err != nil {
		log.Printf("Redis 注册失败，拒绝连接 userId=%s deviceId=%s err=%v\n", userId, deviceId, err)
	}
	defer func() {
		if err = rm.unregister(userId, deviceId, platform, c.connID); err != nil {
			log.Printf("Redis 注销失败 userId=%s deviceId=%s err=%v\n", userId, deviceId, err)
		}
	}()

	// 注册到时间轮，由时间轮检查心跳超时
	if err = tw.registerToTimeWheel(c); err != nil {
		log.Printf("时间轮注册失败，拒绝连接 userId=%s deviceId=%s err=%v\n", userId, deviceId, err)
		return
	}

	// 开启死循环，不断读取消息
	for {
		messageType, p, err := c.ReadMessage()
		if err != nil {
			log.Printf("读取用户 %s(%s) 消息失败或客户端主动断开: %v\n", userId, deviceId, err)
			break
		}

		// 客户端发来文本帧心跳（浏览器 JS 发不了 WebSocket Ping）
		if messageType == 1 {
			c.lastHeartbeat.Store(time.Now().Unix())
			// 相当于是给客户端回 pong，需要客户端自己去与正常消息做区分
			c.enqueueAndWrite(messageType, p)
		}
	}
}

// 解析 JWT 的辅助函数
func validateTokenAndGetUserId(tokenString string) (string, error) {
	// 解析并校验签名
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// 确保加密算法是我们预期的 HMACHS256，即 HmacSHA256
		if method, ok := token.Method.(*jwt.SigningMethodHMAC); !ok || method.Name != "HS256" {
			return nil, fmt.Errorf("签名算法错误")
		}
		return jwtSecret, nil
	})

	if err != nil {
		return "", err
	}

	// 从 Payload (Claims) 中提取 userId
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		if userId, ok := claims["userId"].(string); ok {
			return userId, nil
		}
	}
	return "", fmt.Errorf("token 非法")
}
