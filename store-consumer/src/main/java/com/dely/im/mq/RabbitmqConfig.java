package com.dely.im.mq;

import org.springframework.amqp.core.Binding;
import org.springframework.amqp.core.BindingBuilder;
import org.springframework.amqp.core.DirectExchange;
import org.springframework.amqp.core.Queue;
import org.springframework.amqp.rabbit.core.RabbitTemplate;
import org.springframework.amqp.rabbit.retry.MessageRecoverer;
import org.springframework.amqp.rabbit.retry.RepublishMessageRecoverer;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

@Configuration
public class RabbitmqConfig {

    private static final String ERROR_EXCHANGE = "im.error.exchange";
    private static final String ERROR_QUEUE = "im.error.queue";

    @Bean
    public DirectExchange errorExchange() {
        return new DirectExchange(ERROR_EXCHANGE, true, false);
    }

    @Bean
    public Queue errorQueue() {
        return new Queue(ERROR_QUEUE, true, false, false);
    }

    @Bean
    public Binding errorBinding(Queue errorQueue, DirectExchange errorExchange) {
        return BindingBuilder.bind(errorQueue).to(errorExchange).with("error");
    }

    // 默认重试结束拒绝，这里配置重试结束加入死信队列
    @Bean
    public MessageRecoverer messageRecoverer(RabbitTemplate rabbitTemplate) {
        return new RepublishMessageRecoverer(rabbitTemplate, ERROR_EXCHANGE, "error");
    }

}
