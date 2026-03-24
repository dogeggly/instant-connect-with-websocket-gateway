// 测试时使用，后续改为 mq 接收下行消息

package main

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

// pushHandler 提供给外部调用的 HTTP 推送接口
// 例如: POST /api/push?userId=dogeggly&msg=hello
func pushHandler(w http.ResponseWriter, r *http.Request) {
	// 解析参数
	userId := r.URL.Query().Get("userId")
	msg := r.URL.Query().Get("msg")
	if userId == "" || msg == "" {
		http.Error(w, "参数不完整", http.StatusBadRequest)
		return
	}

	// 从我们的内存管理器中寻找这个用户
	clientMap, exists := cm.get(userId)
	if !exists {
		// 用户不在线（未连在这个网关上）
		http.Error(w, "用户不在线", http.StatusNotFound)
		return
	}

	log.Printf("收到用户 %s 消息: %s\n", userId, msg)

	// 找到了！把消息推送下去
	for _, client := range clientMap {
		err := client.WriteMessage(websocket.TextMessage, []byte(msg))
		if err != nil {
			log.Printf("推送消息失败 userId=%s err=%v\n", userId, err)
		}
	}

	_, err := w.Write([]byte("推送成功!"))
	if err != nil {
		http.Error(w, "推送成功，但响应失败", http.StatusInternalServerError)
	}
}
