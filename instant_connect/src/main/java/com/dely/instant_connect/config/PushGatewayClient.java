package com.dely.instant_connect.config;

import org.springframework.stereotype.Component;
import org.springframework.web.util.UriComponentsBuilder;

import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.util.Objects;

@Component
public class PushGatewayClient {

    private static final Duration PUSH_CONNECT_TIMEOUT = Duration.ofSeconds(2);
    private static final Duration PUSH_REQUEST_TIMEOUT = Duration.ofSeconds(3);

    private final HttpClient httpClient = HttpClient.newBuilder()
            .connectTimeout(PUSH_CONNECT_TIMEOUT)
            .build();

    public boolean push(String gatewayAddress, String msg) {
        URI pushUri = UriComponentsBuilder.newInstance()
                .scheme("http")
                .host(normalizeGatewayHost(gatewayAddress))
                .port(8080)
                .path("/api/push")
                .queryParam("msg", msg)
                .build(true)
                .toUri();

        HttpRequest request = HttpRequest.newBuilder(pushUri)
                .timeout(PUSH_REQUEST_TIMEOUT)
                .POST(HttpRequest.BodyPublishers.noBody())
                .build();

        try {
            HttpResponse<Void> response = httpClient.send(request, HttpResponse.BodyHandlers.discarding());
            return response.statusCode() >= 200 && response.statusCode() < 300;
        } catch (Exception ignored) {
            return false;
        }
    }

    private String normalizeGatewayHost(String gatewayAddress) {
        String trimmed = Objects.requireNonNullElse(gatewayAddress, "").trim();
        if (trimmed.startsWith("http://")) {
            trimmed = trimmed.substring("http://".length());
        } else if (trimmed.startsWith("https://")) {
            trimmed = trimmed.substring("https://".length());
        }

        int slashIndex = trimmed.indexOf('/');
        if (slashIndex >= 0) {
            trimmed = trimmed.substring(0, slashIndex);
        }
        int colonIndex = trimmed.indexOf(':');
        if (colonIndex >= 0) {
            trimmed = trimmed.substring(0, colonIndex);
        }
        return trimmed;
    }
}
