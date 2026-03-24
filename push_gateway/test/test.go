package main

import (
	"fmt"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

const (
	targetIp       = "127.0.0.1"
	port           = ":8080"
	maxConnections = 10000
)

func main() {
	fmt.Printf("开始模拟 %d 个客户端并发连接...\n", maxConnections)

	for i := 0; i < maxConnections; i++ {
		go func(id int) {
			url := fmt.Sprintf("ws://%s%s/ws?userId=%d", targetIp, port, id+10001)

			// 拨号连接网关
			conn, _, err := websocket.DefaultDialer.Dial(url, nil)
			if err != nil {
				log.Printf("连接失败 bot_%d: %v\n", id, err)
				return
			}
			defer conn.Close()

			// 连上后保持静默，只负责接收心跳或推送
			for {
				_, _, err := conn.ReadMessage()
				if err != nil {
					return
				}
			}
		}(i)

		// 稍微停顿一下，防止瞬间发出 1 万个握手请求把虚拟机的网卡 TCP 队列打满
		time.Sleep(2 * time.Millisecond)
	}

	fmt.Println("所有连接已发起，保持挂机中...")
	select {} // 永远阻塞主协程，防止程序退出
}
