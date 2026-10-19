"""Protocol checks against a real mixed listener; never log credentials."""

import base64
import socket


def receive(sock, size):
    data = b""
    while len(data) < size:
        chunk = sock.recv(size - len(data))
        if not chunk:
            break
        data += chunk
    return data


def http_status(host, port, credentials=None):
    header = ""
    if credentials is not None:
        header = "Proxy-Authorization: Basic " + base64.b64encode(credentials).decode() + "\r\n"
    with socket.create_connection((host, port), 3) as sock:
        sock.settimeout(3)
        sock.sendall(("GET http://example.invalid/ HTTP/1.1\r\nHost: example.invalid\r\n" + header + "Connection: close\r\n\r\n").encode())
        return int(sock.recv(128).split(b" ", 2)[1])


def socks_login(sock, username, password):
    sock.sendall(bytes([5, 1, 2]))
    if receive(sock, 2) != bytes([5, 2]):
        return False
    user, secret = username.encode(), password.encode()
    sock.sendall(bytes([1, len(user)]) + user + bytes([len(secret)]) + secret)
    return receive(sock, 2) == bytes([1, 0])


def check_credentials(host, port, username, password):
    checks = {
        "http_missing_rejected": http_status(host, port) == 407,
        "http_wrong_rejected": http_status(host, port, b"invalid-user:invalid-password") in (403, 407),
    }
    with socket.create_connection((host, port), 3) as sock:
        sock.settimeout(3)
        sock.sendall(bytes([5, 1, 0]))
        method = receive(sock, 2)
        if method == bytes([5, 255]):
            checks["socks_missing_rejected"] = True
        elif method == bytes([5, 2]):
            # Mihomo requests credentials even if the client offered only the
            # anonymous method. A client skipping that step must still fail.
            sock.sendall(bytes([5, 1, 0, 3, 15]) + b"example.invalid" + bytes([0, 80]))
            checks["socks_missing_rejected"] = receive(sock, 2) == bytes([1, 1])
        else:
            checks["socks_missing_rejected"] = False
    with socket.create_connection((host, port), 3) as sock:
        sock.settimeout(3)
        checks["socks_wrong_rejected"] = not socks_login(sock, "invalid-user", "invalid-password")
    with socket.create_connection((host, port), 3) as sock:
        sock.settimeout(3)
        checks["socks_valid_accepted"] = socks_login(sock, username, password)
    return checks
