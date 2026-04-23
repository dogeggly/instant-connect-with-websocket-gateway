## 项目结构
```
instant-messaging-with-websocket-gateway/
├── instant-messaging/  # springboot 主业务层
├── store-consumer/     # springboot 消费者，用于完成异步写扩散
├── websocket-gateway/  # go 网关接入层
├── protobuf/           # 消息通信序列化契约
└── sql/                # 建表语句
```