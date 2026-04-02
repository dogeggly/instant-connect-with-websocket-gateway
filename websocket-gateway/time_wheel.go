package main

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// 发送 Ping 心跳包的频率。必须小于 pongWait。
const pingPeriod = 45

// 1. 定义 slot（槽），把锁和数据绑在一起
type slot struct {
	sync.Mutex
	items *list.List
}

// 2. 时间轮结构
type timeWheel struct {
	slots [60]*slot // 数组里存的是改造后的 slot
	tick  int8
}

func initTimeWheel() *timeWheel {
	tw := &timeWheel{}
	for i := range tw.slots {
		tw.slots[i] = &slot{items: list.New()}
	}
	tw.tick = int8(time.Now().Unix() % 60)
	return tw
}

func (tw *timeWheel) startTimeWheel(ctx context.Context) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			currentTick := int8(time.Now().Unix() % 60)
			gap := int(currentTick - tw.tick)
			// 时钟轮转完一圈了
			if gap < 0 {
				gap += 60
			}

			for i := 0; i < gap; i++ {
				tw.tick = (tw.tick + 1) % 60
				slot := tw.slots[tw.tick]
				if slot == nil {
					return errors.New("时间轮槽位未初始化")
				}

				slot.Lock()
				oldList := slot.items
				slot.items = list.New()
				slot.Unlock()

				// 网络 I/O 操作放在锁外
				go pingSender(oldList, tw.tick)
			}
		}
	}
}

func pingSender(l *list.List, baseTick int8) {
	if l == nil || l.Len() == 0 {
		return
	}

	for e := l.Front(); e != nil; {
		next := e.Next()
		c, ok := e.Value.(*client)
		if !ok || c == nil || c.isClose.Load() {
			l.Remove(e)
			e = next
			continue
		}

		c.enqueueAndWrite(websocket.PingMessage, nil)
		e = next
	}

	if l.Len() == 0 {
		return
	}

	nextTick := (baseTick + pingPeriod) % 60
	nextSlot := tw.slots[nextTick]
	if nextSlot == nil {
		return
	}

	nextSlot.Lock()
	nextSlot.items.PushBackList(l)
	nextSlot.Unlock()
}

func (tw *timeWheel) registerToTimeWheel(c *client) {
	if c == nil || c.isClose.Load() {
		return
	}

	nextTick := (time.Now().Unix()%60 + pingPeriod) % 60
	slot := tw.slots[nextTick]
	if slot == nil {
		return
	}

	slot.Lock()
	slot.items.PushBack(c)
	slot.Unlock()
}
