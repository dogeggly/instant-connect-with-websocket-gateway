# store-consumer 落库 TPS 压测指南

## 原理

直接向 `im.direct.store.exchange` 投递 Protobuf 消息，然后轮询 `im.store.queue` 深度直到排空：

```
消费速率 (msg/s) = 消息总数 / 排空耗时
落库 TPS (行/s)  = 消费速率 × 每消息行数
```

## 依赖服务

| 服务 | 地址 | 确认方式 |
|------|------|---------|
| RabbitMQ | `192.168.100.131:5672` | 浏览器打开 `http://192.168.100.131:15672` |
| Redis | `127.0.0.1:6379` (db 3) | `.\redis-cli.exe -n 3 PING` |
| PostgreSQL | `192.168.100.131:5432` | `psql -h 192.168.100.131 -U postgres -d instant_messaging -c "SELECT 1"` |

> 如果你的 `redis-cli` 和 `psql` 不在 PATH 里，用完整路径替代。

## 步骤

### 1. 启动 store-consumer

```powershell
cd store-consumer
mvn spring-boot:run
```

看到 `Started StoreConsumerApplication` 即可。

### 2. 预置群聊数据

**只有测 group10 / group100 才需要这一步，单聊跳过。**

打开一个新的 **PowerShell** 窗口（不是 redis-cli 里面），执行：

```powershell
# 10 人群组（group_id=50001，成员 50001~50010）
PS> .\redis-cli.exe -n 3 SADD "im:group_members:50001" 50001 50002 50003 50004 50005 50006 50007 50008 50009 50010
PS> .\redis-cli.exe -n 3 EXPIRE "im:group_members:50001" 3600

# 100 人群组（group_id=50002，成员 50011~50110）
PS> for ($i=50011; $i -le 50110; $i++) { .\redis-cli.exe -n 3 SADD "im:group_members:50002" $i }
PS> .\redis-cli.exe -n 3 EXPIRE "im:group_members:50002" 3600
```

验证（PowerShell 中执行）：
```powershell
PS> .\redis-cli.exe -n 3 SCARD "im:group_members:50001"   # 预期 10
PS> .\redis-cli.exe -n 3 SCARD "im:group_members:50002"   # 预期 100
```

### 3. 运行压测

```powershell
cd store-consumer\test

# 先用小量验证流程
load_test.exe -scenario=single -count=1000

# 正式测试
load_test.exe -scenario=single -count=5000
load_test.exe -scenario=group10 -count=1000
load_test.exe -scenario=group100 -count=200
```

### 4. 解读输出

```
╔══════════════════════════════════╗
║         测 试 结 果             ║
╠══════════════════════════════════╣
║ 发布耗时:    2.3s               ║  ← 灌消息花了多久
║ 发布速率:    2174 msg/s         ║  ← 灌消息速度（仅供参考）
╠══════════════════════════════════╣
║ 消费耗时:    10.5s              ║  ← 消费者排空队列的时间 ★
║ 消费速率:    476 msg/s          ║  ← = 消息总数 / 消费耗时 ★★
║ 落库 TPS:    952 行/s          ║  ← = 消费速率 × 每消息行数 ★★★
╚══════════════════════════════════╝
```

**简历用「消费速率」和「落库 TPS」。**

## 清理

```powershell
# 删除测试产生的 timeline 数据
psql -h 192.168.100.131 -U postgres -d instant_messaging -c "DELETE FROM timeline WHERE owner_id >= 40000"
```

## 常见问题

| 现象 | 原因 | 解决 |
|------|------|------|
| 队列不减少、60 秒超时 | store-consumer 没启动 | `mvn spring-boot:run` |
| 日志提示「群 X 无成员」 | 没预置 Redis 数据 | 执行步骤 2 |
| `connect: connection refused` | RabbitMQ / Redis / PG 没启动 | 检查服务 |
