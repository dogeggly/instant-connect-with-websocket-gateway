package com.dely.instant_connect.config;

import lombok.extern.slf4j.Slf4j;
import org.springframework.amqp.core.DirectExchange;
import org.springframework.amqp.support.converter.Jackson2JsonMessageConverter;
import org.springframework.amqp.support.converter.MessageConverter;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

@Slf4j
@Configuration
public class RabbitmqConfig {

    public static final String GATEWAY_EXCHANGE = "ic.gateway.direct.exchange";
    public static final String GATEWAY_QUEUE = "ws.gateway.direct.queue";

    @Bean
    public MessageConverter messageConverter() {
        return new Jackson2JsonMessageConverter();
    }

    @Bean
    public DirectExchange gatewayExchange() {
        // 参数：name, durable(持久化), autoDelete(自动删除)
        // 交换机需要持久化，因为它是基础设施
        return new DirectExchange(GATEWAY_EXCHANGE, true, false);
    }

}
