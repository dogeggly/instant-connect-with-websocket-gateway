package main

import "log"

// cleanupMyRoutes 优雅关闭时遍历本地连接管理器，删除本节点在 Redis 中的路由。
func cleanupMyRoutes() {
	var cleaned int

	for _, shard := range cm.connections {
		shard.RLock()
		for userId, devices := range shard.items {
			for deviceId, c := range devices {
				if c == nil {
					continue
				}
				if err := rm.unregister(userId, deviceId, c.platform, c.connID); err != nil {
					log.Printf("注销失败 userId=%s deviceId=%s err=%v", userId, deviceId, err)
					continue
				}
				cleaned++
			}
		}
		shard.RUnlock()
	}

	log.Printf("清扫完成, nodeId=%d, cleaned=%d", nodeId, cleaned)
}
