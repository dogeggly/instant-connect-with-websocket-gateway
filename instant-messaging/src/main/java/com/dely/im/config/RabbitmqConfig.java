package com.dely.im.config;

import lombok.extern.slf4j.Slf4j;
import org.springframework.amqp.core.DirectExchange;
import org.springframework.amqp.core.FanoutExchange;
import org.springframework.boot.autoconfigure.amqp.RabbitTemplateCustomizer;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

@Slf4j
@Configuration
public class RabbitmqConfig {

    public static final String DIRECT_EXCHANGE = "im.direct.exchange";
    public static final String FANOUT_EXCHANGE = "im.fanout.exchange";
    public static final String DIRECT_STORE_EXCHANGE = "im.direct.store.exchange";

    @Bean
    public DirectExchange directExchange() {
        // 参数：name, durable(持久化), autoDelete(自动删除)
        // 交换机需要持久化，因为它是基础设施
        log.info("初始化网关 direct 交换机");
        return new DirectExchange(DIRECT_EXCHANGE, true, false);
    }

    @Bean
    public FanoutExchange fanoutExchange() {
        log.info("初始化网关 fanout 交换机");
        return new FanoutExchange(FANOUT_EXCHANGE, true, false);
    }

    @Bean
    public DirectExchange directStoreExchange() {
        log.info("初始化网关 direct_store 交换机");
        return new DirectExchange(DIRECT_STORE_EXCHANGE, true, false);
    }

    @Bean
    public RabbitTemplateCustomizer myRabbitTemplateCustomizer() {
        return rabbitTemplate -> {
            rabbitTemplate.setConfirmCallback((correlationData, ack, cause) -> {
                if (!ack) {
                    // 走到这里，说明 RabbitMQ 服务器本身出大问题了（比如磁盘满了、内部异常）
                    // 降级成离线拉取
                    log.error("消息未能到达 Exchange! 原因: {}", cause);
                }
            });
            rabbitTemplate.setReturnsCallback(returnedMessage -> {
                // 当 Go 网关宕机，它专属的 autoDelete 队列被销毁后，
                // 交换机拿着 targetNodeId (RoutingKey) 找不到任何队列，就会把消息原路退回到这里！
                String targetNodeId = returnedMessage.getRoutingKey();
                String payload = new String(returnedMessage.getMessage().getBody());
                log.warn("目标网关节点 {} 的队列不存在，被退回的消息载荷: {}", targetNodeId, payload);
            });
        };
    }

}
