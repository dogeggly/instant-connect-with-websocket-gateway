package main

import (
	"errors"
	"log"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// 允许等待客户端 Pong 响应的最大时间。过了这个时间没收到心跳，就踢掉它。
	pongWait = 60 * time.Second
	// 发送 Ping 心跳包的频率。必须小于 pongWait。
	pingPeriod = 45 * time.Second
)

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
	c := &client{Conn: conn, connID: connID}
	// 草泥马别给我处理 err 的警告了
	defer func() {
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

	// 设置首次读取的绝对超时时间 (当前时间 + 60秒)
	err = c.SetReadDeadline(time.Now().Add(pongWait))
	if err != nil {
		log.Println("设置读取超时失败:", err)
		return
	}

	// 注册 Pong 处理器：一旦收到客户端的 Pong 心跳响应，就续命 60 秒！
	c.SetPongHandler(func(_ string) error {
		// 调试时开启
		// log.Printf("收到 %s 的心跳 Pong 响应，为其续命...\n", userId)
		err = c.SetReadDeadline(time.Now().Add(pongWait))
		if err != nil {
			log.Printf("续命 websocket 连接失败 userId=%s deviceId=%s err=%v\n", userId, deviceId, err)
			return err
		}
		err = rm.keepAlive(userId, deviceId, platform, c)
		if err != nil {
			if errors.Is(err, errKeepAliveQueueFull) {
				log.Printf("keepalive 队列满，续期失败 userId=%s deviceId=%s\n", userId, deviceId)
				return err
			}
			log.Printf("续命 Redis 失败 userId=%s deviceId=%s err=%v\n", userId, deviceId, err)
			return err
		}
		return nil
	})

	// 开启一个专属的 Goroutine，定时给客户端发 Ping 包
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			<-ticker.C // 每 45 秒执行一次
			// 发送标准的 Ping 控制帧
			if err := c.WriteMessage(websocket.PingMessage, nil); err != nil {
				return // 如果发心跳失败（说明连接已坏），退出协程
			}
		}
	}()

	// 开启死循环，不断读取和发送消息 (Goroutine 的轻量级体现在这里，死循环不会卡死其他用户)
	for {
		// 阻塞读取客户端发来的消息
		messageType, p, err := c.ReadMessage()
		if err != nil {
			log.Printf("读取用户 %s(%s) 消息失败或客户端主动断开: %v\n", userId, deviceId, err)
			break // 报错了就跳出循环，触发上面的 defer 关闭连接
		}

		// 实际业务不需要这段，websocket 只单向发消息
		if messageType != 1 {
			err = c.WriteMessage(websocket.TextMessage, []byte("目前支持发送文字帧"))
			if err != nil {
				log.Println("写入消息失败:", err)
				break
			}
		}
		log.Printf("收到用户 %s(%s) 消息: %s\n", userId, deviceId, string(p))
		err = c.WriteMessage(messageType, p) // 原封不动地写回给客户端
		if err != nil {
			log.Println("写入消息失败:", err)
			break
		}
	}
}
