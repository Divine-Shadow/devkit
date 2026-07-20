#!/usr/bin/env python3
"""Hermetic CONNECT peer for the Product adapter lifecycle VM."""

import socket
import sys
import threading


def copy(source, target):
    try:
        while True:
            data = source.recv(65536)
            if not data:
                try:
                    target.shutdown(socket.SHUT_WR)
                except OSError:
                    pass
                return
            target.sendall(data)
    except OSError:
        return


def serve(client, upstream_port):
    with client:
        request = b""
        while b"\r\n\r\n" not in request:
            chunk = client.recv(4096)
            if not chunk:
                return
            request += chunk
            if len(request) > 16384:
                return
        first = request.split(b"\r\n", 1)[0]
        if first != b"CONNECT ssh.github.com:443 HTTP/1.1":
            client.sendall(b"HTTP/1.1 403 Forbidden\r\n\r\n")
            return
        with socket.create_connection(("127.0.0.1", upstream_port), timeout=5) as server:
            client.sendall(b"HTTP/1.1 200 Connection Established\r\n\r\n")
            left = threading.Thread(target=copy, args=(client, server), daemon=True)
            right = threading.Thread(target=copy, args=(server, client), daemon=True)
            left.start()
            right.start()
            left.join()
            right.join()


def main():
    if len(sys.argv) != 3:
        raise SystemExit("usage: proxy LISTEN_PORT UPSTREAM_PORT")
    listen_port = int(sys.argv[1])
    upstream_port = int(sys.argv[2])
    listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    listener.bind(("127.0.0.1", listen_port))
    listener.listen(16)
    while True:
        client, _ = listener.accept()
        threading.Thread(target=serve, args=(client, upstream_port), daemon=True).start()


if __name__ == "__main__":
    main()
