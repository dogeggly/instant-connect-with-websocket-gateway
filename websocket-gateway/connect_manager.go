package main

import (
	"log"
	"sync"
)

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
