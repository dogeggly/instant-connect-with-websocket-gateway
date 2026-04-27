package main

import (
	"hash/fnv"
	"log"
	"sync"
)

// connectionManager 管理所有在线的 WebSocket 连接
type connectionManager struct {
	// 分片 map：按 userId 哈希到固定分片，降低全局锁竞争
	connections [256]*Shard
}

type Shard struct {
	sync.RWMutex
	items map[string]map[string]*client
}

// newConnectionManager 初始化管理器
func newConnectionManager() *connectionManager {
	cm := &connectionManager{}
	for i := range cm.connections {
		cm.connections[i] = &Shard{items: make(map[string]map[string]*client)}
	}
	return cm
}

func (cm *connectionManager) shard(userId string) *Shard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(userId))
	idx := h.Sum32() % uint32(len(cm.connections))
	return cm.connections[idx]
}

// add 添加/注册新连接 (写操作，用完全锁 Lock)
func (cm *connectionManager) add(userId string, deviceId string, c *client) {
	shard := cm.shard(userId)
	shard.Lock()
	defer shard.Unlock()
	if shard.items[userId] == nil {
		shard.items[userId] = make(map[string]*client)
	}
	if oldClient, exists := shard.items[userId][deviceId]; exists {
		// 发现本地已经有同一个设备了！
		// 物理斩断旧连接的 Socket！
		// 这会瞬间引发 oldClient 后台的 ReadMessage 抛出 error，
		// 从而触发旧协程跳出死循环并完美退出，彻底释放资源。
		_ = oldClient.Close()
	}
	shard.items[userId][deviceId] = c
	host := c.RemoteAddr().String()
	log.Printf("[上线] 用户 %s(%s) 已连接，ip 和端口号: %s\n", userId, deviceId, host)
}

// remove 移除断开的连接 (写操作，用完全锁 Lock)
func (cm *connectionManager) remove(userId string, deviceId string, c *client) {
	shard := cm.shard(userId)
	shard.Lock()
	defer shard.Unlock()
	if devices, exists := shard.items[userId]; exists {
		if currentClient, exists := devices[deviceId]; exists {
			// 只有当 Map 里当前的连接，和准备退出的连接是同一个内存地址时，才允许删除！
			if currentClient == c {
				delete(devices, deviceId)
				host := c.RemoteAddr().String()
				log.Printf("[下线] 用户 %s(%s) 已断开，历史 ip 和端口号: %s\n", userId, deviceId, host)
				// 如果这个用户的所有设备都下线了，把外层 Map 的 Key 也清理掉，防止内存泄漏
				if len(devices) == 0 {
					delete(shard.items, userId)
				}
			}
			// 如果指针不同，说明自己已经被“顶号”了，对不碰现有的 Map 数据！
		}
	}
}

// get 获取指定用户的连接 (读操作，用读锁 RLock，只有当所有读锁释放后，才能上写锁)
func (cm *connectionManager) get(userId string) (map[string]*client, bool) {
	shard := cm.shard(userId)
	shard.RLock()
	defer shard.RUnlock()
	if shard.items[userId] == nil {
		return nil, false
	}
	clientMap, exist := shard.items[userId]
	return clientMap, exist
}
