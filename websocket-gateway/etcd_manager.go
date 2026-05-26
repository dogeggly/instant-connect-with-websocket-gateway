package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	etcdAddr        = "192.168.100.131:2379"
	etcdDialTimeout = 5 * time.Second
	etcdNodePrefix  = "/ws/nodes/"
	etcdLeaseTTL    = 5 // 秒
)

type etcdManager struct {
	*clientv3.Client
	leaseID clientv3.LeaseID
}

// newEtcdManager 连接 etcd，创建租约。
func newEtcdManager() (*etcdManager, error) {
	cli, err := clientv3.New(clientv3.Config{
		// 实际生产中应考虑集群部署
		Endpoints:   []string{etcdAddr},
		DialTimeout: etcdDialTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("连接 etcd 失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), etcdDialTimeout)
	defer cancel()
	leaseResp, err := cli.Grant(ctx, etcdLeaseTTL)
	if err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("etcd 创建租约失败: %w", err)
	}

	em := &etcdManager{Client: cli, leaseID: leaseResp.ID}

	return em, nil
}

// keepAliveLoop 内部续约循环。
func (em *etcdManager) keepAliveLoop(ctx context.Context) error {
	ch, err := em.KeepAlive(ctx, em.leaseID)
	if err != nil {
		return fmt.Errorf("etcd KeepAlive 启动失败: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case ka, ok := <-ch:
			if !ok || ka == nil {
				return errors.New("etcd KeepAlive 通道关闭，租约已失效")
			}
		}
	}
}

// Close 撤销租约（连带删除注册 key），关闭连接。
func (em *etcdManager) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), etcdDialTimeout)
	defer cancel()
	if _, err := em.Revoke(ctx, em.leaseID); err != nil {
		log.Printf("etcd 释放租约失败, leaseID=%d, err=%v", em.leaseID, err)
	} else {
		log.Printf("etcd 已释放, leaseID=%d", em.leaseID)
	}
	return em.Client.Close()
}

// registerNode 在 etcd 中注册本节点，key 绑定租约。
func (em *etcdManager) registerNode() (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), etcdDialTimeout)
	defer cancel()

	key := fmt.Sprintf("%s%d", etcdNodePrefix, em.leaseID)
	_, err := em.Put(ctx, key, "alive", clientv3.WithLease(em.leaseID))
	if err != nil {
		return -1, fmt.Errorf("etcd 注册节点失败: %w", err)
	}

	return int64(em.leaseID), nil
}
