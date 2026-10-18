"""Operator-started stdio host. An agent receives pipes, never this environment."""

import argparse
import json
import os
import stat
import sys

from github_auth.boundary import (BoundaryError, Config, EnvironmentJwtSource,
                                  GitHubTransport, TokenBroker, require)
from implementer.gateway import Authorization, ImplementerGateway


class JsonLineAudit:
    """Private append-only local sink; production retention is an operator concern."""

    def __init__(self, path):
        self._fd = None
        try:
            self._fd = os.open(path, os.O_WRONLY | os.O_APPEND | os.O_CREAT
                               | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
            info = os.fstat(self._fd)
            require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                    and info.st_mode & 0o077 == 0, "audit_unavailable")
        except Exception:
            self.close()
            raise BoundaryError("audit_unavailable") from None

    def __call__(self, event):
        try:
            line = (json.dumps(event, separators=(",", ":")) + "\n").encode()
            require(os.write(self._fd, line) == len(line), "audit_unavailable")
            os.fsync(self._fd)
        except Exception:
            raise BoundaryError("audit_unavailable") from None

    def close(self):
        if self._fd is not None:
            os.close(self._fd)
            self._fd = None


def serve(gateway, source, target):
    """One JSON object per line: {operation, arguments}. No host/config controls."""
    while True:
        # Bound memory and terminate an oversized stream rather than draining it.
        line = source.readline(1_048_577)
        if not line:
            return
        if len(line) > 1_048_576:
            target.write('{"error":"request_invalid"}\n')
            target.flush()
            return
        try:
            request = json.loads(line)
            require(type(request) is dict and set(request) == {"operation", "arguments"}
                    and isinstance(request["operation"], str)
                    and type(request["arguments"]) is dict, "operation_arguments_invalid")
            response = {"result": gateway.execute(request["operation"], **request["arguments"])}
        except BoundaryError as error:
            response = {"error": str(error)}
        except Exception:
            response = {"error": "request_invalid"}
        target.write(json.dumps(response) + "\n")
        target.flush()


def main():
    parser = argparse.ArgumentParser(description="Trusted operator-only Implementer broker")
    parser.add_argument("--authorization-file", required=True,
                        help="Operator-owned JSON Authorization object; never agent supplied")
    parser.add_argument("--audit-file", required=True, help="Private JSONL audit destination")
    parser.add_argument("--allow-network", action="store_true",
                        help="Requires separate human-approved activation and evidence record")
    args = parser.parse_args()
    audit = None
    try:
        config = Config.from_env(os.environ)
        # No dynamic code, commands, plugins, secret endpoints or user-supplied URLs.
        with open(args.authorization_file, encoding="utf-8") as file:
            raw = file.read(8193)
        require(len(raw) <= 8192, "authorization_invalid")
        authorization = Authorization(**json.loads(raw))
        require(config.jwt_source.startswith("env:"), "secret_source_unavailable")
        audit = JsonLineAudit(args.audit_file)
        broker = TokenBroker(config, GitHubTransport(allow_network=args.allow_network),
                             EnvironmentJwtSource())
        serve(ImplementerGateway(config, authorization, broker, audit), sys.stdin, sys.stdout)
        return 0
    except Exception:
        # CLI parsing errors contain no environment values; runtime errors stay fixed.
        sys.stderr.write("broker_startup_or_stream_failed\n")
        return 1
    finally:
        if audit is not None:
            audit.close()


if __name__ == "__main__":
    raise SystemExit(main())
