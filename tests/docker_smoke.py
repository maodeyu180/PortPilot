#!/usr/bin/env python3
"""Exercise a fresh loopback-only Docker installation with a local upstream.

Uses only Python's standard library. The script refuses populated stores and
the normal controller port; never point it at a production installation.
"""

import argparse
import base64
import http.client
import http.cookiejar
import json
from pathlib import Path
import secrets
import socket
import socketserver
import threading
import urllib.error
import urllib.parse
import urllib.request

from proxy_auth import check_credentials, receive, socks_login


BODY = b"portpilot-docker-smoke-ok"


class Upstream(socketserver.StreamRequestHandler):
    def handle(self):
        self.request.settimeout(5)
        first = self.rfile.readline(8192)
        while self.rfile.readline(8192) not in (b"\r\n", b"\n", b""):
            pass
        if first.startswith(b"CONNECT "):
            self.wfile.write(b"HTTP/1.1 200 Connection Established\r\n\r\n")
            self.wfile.flush()
            self.rfile.readline(8192)
            while self.rfile.readline(8192) not in (b"\r\n", b"\n", b""):
                pass
        self.wfile.write(b"HTTP/1.1 200 OK\r\nContent-Length: " + str(len(BODY)).encode() + b"\r\nConnection: close\r\n\r\n" + BODY)
        self.wfile.flush()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default="http://127.0.0.1:18787")
    parser.add_argument("--env-file", default=".env")
    parser.add_argument("--proxy-port", type=int, default=31001)
    args = parser.parse_args()
    base = args.base_url.rstrip("/")
    address = urllib.parse.urlsplit(base)
    assert address.scheme == "http" and address.hostname == "127.0.0.1", "test panel must use loopback"
    values = dict(line.split("=", 1) for line in Path(args.env_file).read_text().splitlines() if line and not line.startswith("#") and "=" in line)
    password = values.get("PORTPILOT_ADMIN_PASSWORD", "")
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def api(path, method="GET", data=None, expected=200):
        encoded = None if data is None else json.dumps(data).encode()
        req = urllib.request.Request(base + "/api" + path, data=encoded, method=method, headers={"Content-Type": "application/json", "Origin": base})
        try:
            response = opener.open(req, timeout=15)
        except urllib.error.HTTPError as error:
            response = error
        assert response.code == expected, f"{method} {path}: unexpected HTTP {response.code}"
        return json.loads(response.read())

    api("/state", expected=401)
    session = api("/session")
    if session.get("setupRequired"):
        password = secrets.token_hex(24)
        setup = {"password": password, "confirmPassword": password}
        api("/login", "POST", {"password": ""}, expected=409)
        api("/setup", "POST", {**setup, "password": "short", "confirmPassword": "short"}, expected=400)
        api("/setup", "POST", {**setup, "confirmPassword": "does-not-match"}, expected=400)
        assert api("/session")["setupRequired"], "invalid setup unlocked the panel"
        api("/setup", "POST", setup)
        assert api("/session")["authenticated"], "setup session missing"
        api("/setup", "POST", setup, expected=409)
        api("/logout", "POST", {})
        api("/state", expected=401)
    assert len(password) >= 12, "test requires a configured management password"
    api("/login", "POST", {"password": password})
    state = api("/state")
    settings = state["settings"]
    assert not state["nodes"] and not state["subscriptions"], "test requires an empty installation"
    assert settings["publicHost"] == "127.0.0.1" and settings["controllerUrl"] != "http://127.0.0.1:9090", "use an isolated test controller"
    assert api("/core")["connected"], "Mihomo did not start from bootstrap config"
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", args.proxy_port))
    upstream = socketserver.ThreadingTCPServer(("127.0.0.1", 0), Upstream)
    upstream.daemon_threads = True
    worker = threading.Thread(target=upstream.serve_forever, daemon=True)
    worker.start()
    fixture_id = None
    try:
        settings.update(publicHost="127.0.0.1", listenHost="127.0.0.1", portStart=args.proxy_port, portEnd=args.proxy_port + 4)
        api("/settings", "PUT", settings)
        for bad in ("", "short"):
            api("/settings", "PUT", {**settings, "password": bad}, expected=400)
        node = {"name": "Docker smoke local upstream", "type": "http", "server": "127.0.0.1", "port": upstream.server_address[1]}
        state = api("/nodes", "POST", {"yaml": json.dumps(node), "port": args.proxy_port}, expected=201)
        fixture_id = state["nodes"][0]["id"]
        api("/core/apply", "POST", {})
        checks = check_credentials("127.0.0.1", args.proxy_port, settings["username"], settings["password"])
        assert all(checks.values()), "listener authentication failed: " + str(checks)
        credentials = (settings["username"] + ":" + settings["password"]).encode()
        connection = http.client.HTTPConnection("127.0.0.1", args.proxy_port, timeout=5)
        connection.request("GET", "http://example.test/probe", headers={"Host": "example.test", "Proxy-Authorization": "Basic " + base64.b64encode(credentials).decode()})
        response = connection.getresponse()
        assert response.status == 200 and response.read() == BODY, "authenticated HTTP forwarding failed"
        connection.close()
        with socket.create_connection(("127.0.0.1", args.proxy_port), 5) as sock:
            sock.settimeout(5)
            assert socks_login(sock, settings["username"], settings["password"])
            domain = b"example.test"
            sock.sendall(bytes([5, 1, 0, 3, len(domain)]) + domain + bytes([0, 80]))
            reply = receive(sock, 4)
            assert reply[:2] == bytes([5, 0]), "authenticated SOCKS5H connect failed"
            if reply[3] == 1:
                receive(sock, 6)
            elif reply[3] == 4:
                receive(sock, 18)
            elif reply[3] == 3:
                receive(sock, receive(sock, 1)[0] + 2)
            else:
                raise AssertionError("invalid SOCKS5 reply")
            sock.sendall(b"GET /probe HTTP/1.1\r\nHost: example.test\r\nConnection: close\r\n\r\n")
            response = http.client.HTTPResponse(sock)
            response.begin()
            assert response.status == 200 and response.read() == BODY, "SOCKS5H forwarding failed"
        for protocol in ("http", "socks5h"):
            link = api("/export", "POST", {"protocol": protocol, "ids": [state["nodes"][0]["id"]]})["links"]
            parsed = urllib.parse.urlsplit(link)
            assert parsed.scheme == protocol and urllib.parse.unquote(parsed.username) == settings["username"] and urllib.parse.unquote(parsed.password) == settings["password"]
        # Leave a disabled fixture so a stopped sandbox cannot retain an active
        # proxy pointing at the short-lived upstream after this test exits.
        api("/nodes/" + fixture_id, "PATCH", {"enabled": False})
        api("/core/apply", "POST", {})
        fixture_id = None
        print(json.dumps({"bootstrap": "ok", "mandatory_credentials": checks, "http_forwarding": "ok", "socks5h_forwarding": "ok", "authenticated_exports": "ok", "fixture_disabled": True}))
    finally:
        if fixture_id is not None:
            try:
                api("/nodes/" + fixture_id, "PATCH", {"enabled": False})
                api("/core/apply", "POST", {})
            except Exception:
                print("Fixture cleanup failed; stop the isolated Compose test project.")
        upstream.shutdown()
        upstream.server_close()


if __name__ == "__main__":
    main()
