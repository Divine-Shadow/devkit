FROM alpine:3.19
RUN apk add --no-cache tinyproxy bash netcat-openbsd && \
    mkdir -p /var/run/tinyproxy /var/log/tinyproxy
COPY kit/proxy/tinyproxy.conf /etc/tinyproxy/tinyproxy.conf
COPY kit/proxy/allowlist.txt /etc/tinyproxy/allowlist.txt
EXPOSE 8888
CMD ["tinyproxy", "-d", "-c", "/etc/tinyproxy/tinyproxy.conf"]

