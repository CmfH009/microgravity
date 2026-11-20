#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Microgravity Master Mesh Client
Provides asynchronous and synchronous client interfaces for Microgravity AI agents
to publish agent thoughts, tool calls, workflow steps, and browser context to the
Antigravity Universal EventBus (ws://127.0.0.1:8765).
"""

import os
import sys
import json
import time
import socket
import base64
import asyncio
import logging
import argparse
from typing import Dict, Any, Optional

# Reconfigure stdout for UTF-8 on Windows
if sys.stdout and hasattr(sys.stdout, "reconfigure"):
    try:
        sys.stdout.reconfigure(encoding="utf-8")
    except Exception:
        pass

logger = logging.getLogger("MicrogravityMeshClient")

# Attempt importing MicrogravityEventBusClient from local package
try:
    from eventbus.client import MicrogravityEventBusClient
    HAS_EVENTBUS_CLIENT = True
except ImportError:
    try:
        from microgravity_eventbus import MicrogravityEventBusClient
        HAS_EVENTBUS_CLIENT = True
    except ImportError:
        MicrogravityEventBusClient = None
        HAS_EVENTBUS_CLIENT = False


def emit_event_sync(topic: str, payload: dict, host: str = "127.0.0.1", port: int = 8765, timeout: float = 0.5) -> bool:
    """
    Synchronously transmits an event to the EventBus hub via raw TCP socket.
    Uses WebSocket frame masking with a strict timeout. Returns False safely if offline.
    """
    envelope = {
        "topic": topic,
        "source": "microgravity",
        "timestamp": time.time(),
        "payload": payload
    }
    try:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
            s.settimeout(timeout)
            s.connect((host, port))
            key = base64.b64encode(os.urandom(16)).decode("ascii")
            req = (
                f"GET / HTTP/1.1\r\n"
                f"Host: {host}:{port}\r\n"
                f"Upgrade: websocket\r\n"
                f"Connection: Upgrade\r\n"
                f"Sec-WebSocket-Key: {key}\r\n"
                f"Sec-WebSocket-Version: 13\r\n\r\n"
            )
            s.sendall(req.encode("utf-8"))
            resp = s.recv(1024)
            if b"101 Switching Protocols" in resp:
                msg = json.dumps(envelope).encode("utf-8")
                mask = os.urandom(4)
                header = bytearray([0x81])
                l = len(msg)
                if l < 126:
                    header.append(0x80 | l)
                elif l < 65536:
                    header.append(0x80 | 126)
                    header.extend(l.to_bytes(2, byteorder="big"))
                else:
                    header.append(0x80 | 127)
                    header.extend(l.to_bytes(8, byteorder="big"))
                header.extend(mask)
                masked = bytes(b ^ mask[i % 4] for i, b in enumerate(msg))
                s.sendall(header + masked)
                return True
    except Exception:
        pass
    return False


def emit_thought_sync(thought: str, model: str = "gemini-2.5-pro", turn: int = 1, port: int = 8765) -> bool:
    """Convenience synchronous helper to emit an agent thought."""
    return emit_event_sync("agent.thought", {
        "thought": thought,
        "model": model,
        "step": turn,
        "timestamp": time.time()
    }, port=port)


def emit_tool_call_sync(tool: str, args: dict, result: Optional[Any] = None, port: int = 8765) -> bool:
    """Convenience synchronous helper to emit an agent tool call."""
    return emit_event_sync("agent.tool_call", {
        "tool": tool,
        "args": args,
        "result": result,
        "timestamp": time.time()
    }, port=port)


class MicrogravityMeshClient:
    """
    High-level agent telemetry client for Microgravity.
    Publishes thoughts, tool calls, and workflow turns over the EventBus mesh.
    """

    def __init__(self, agent_id: str = "microgravity_agent", hub_url: str = "ws://127.0.0.1:8765"):
        self.agent_id = agent_id
        self.hub_url = hub_url
        self._bus_client = None
        if HAS_EVENTBUS_CLIENT and MicrogravityEventBusClient:
            self._bus_client = MicrogravityEventBusClient(agent_id=self.agent_id, server_url=self.hub_url)

    def build_thought_event(self, thought: str, model: str = "gemini-2.5-pro", turn: int = 1, context: Optional[dict] = None) -> dict:
        """Constructs a normalized agent.thought event envelope."""
        return {
            "topic": "agent.thought",
            "source": self.agent_id,
            "timestamp": time.time(),
            "payload": {
                "agent_id": self.agent_id,
                "thought": thought,
                "model": model,
                "step": turn,
                "context": context or {}
            }
        }

    def build_tool_call_event(self, tool: str, args: dict, result: Optional[Any] = None, duration_ms: Optional[float] = None) -> dict:
        """Constructs a normalized agent.tool_call event envelope."""
        return {
            "topic": "agent.tool_call",
            "source": self.agent_id,
            "timestamp": time.time(),
            "payload": {
                "agent_id": self.agent_id,
                "tool": tool,
                "args": args,
                "result": result,
                "duration_ms": duration_ms
            }
        }

    def build_step_event(self, step: int, description: str, status: str = "running") -> dict:
        """Constructs a normalized agent.step event envelope."""
        return {
            "topic": "agent.step",
            "source": self.agent_id,
            "timestamp": time.time(),
            "payload": {
                "agent_id": self.agent_id,
                "step": step,
                "description": description,
                "status": status
            }
        }

    def build_browser_event(self, url: str, title: str, active: bool = True) -> dict:
        """Constructs a normalized browser.tab event envelope."""
        return {
            "topic": "browser.tab",
            "source": self.agent_id,
            "timestamp": time.time(),
            "payload": {
                "url": url,
                "title": title,
                "active": active
            }
        }

    async def start(self, wait_connected: bool = False, timeout: float = 2.0):
        """Starts the underlying WebSocket supervisor loop if available."""
        if self._bus_client:
            await self._bus_client.start(wait_connected=wait_connected, timeout=timeout)

    async def stop(self):
        """Stops the underlying WebSocket client cleanly."""
        if self._bus_client:
            await self._bus_client.stop()

    async def publish_thought(self, thought: str, model: str = "gemini-2.5-pro", turn: int = 1, context: Optional[dict] = None):
        """Publishes an agent thought to ws://127.0.0.1:8765."""
        event = self.build_thought_event(thought, model=model, turn=turn, context=context)
        if self._bus_client and self._bus_client.is_connected:
            await self._bus_client.publish(event["topic"], event["payload"])
        else:
            emit_event_sync(event["topic"], event["payload"])

    async def publish_tool_call(self, tool: str, args: dict, result: Optional[Any] = None, duration_ms: Optional[float] = None):
        """Publishes an agent tool call to ws://127.0.0.1:8765."""
        event = self.build_tool_call_event(tool, args, result=result, duration_ms=duration_ms)
        if self._bus_client and self._bus_client.is_connected:
            await self._bus_client.publish(event["topic"], event["payload"])
        else:
            emit_event_sync(event["topic"], event["payload"])

    async def publish_step(self, step: int, description: str, status: str = "running"):
        """Publishes an agent workflow step update."""
        event = self.build_step_event(step, description, status=status)
        if self._bus_client and self._bus_client.is_connected:
            await self._bus_client.publish(event["topic"], event["payload"])
        else:
            emit_event_sync(event["topic"], event["payload"])


async def run_demo_simulation(client: MicrogravityMeshClient):
    """Executes a simulated 3-step agent reasoning workflow over the mesh."""
    print("🚀 Starting Microgravity Live Mesh Telemetry Simulation...")
    await client.start(wait_connected=False)

    steps = [
        ("Evaluating workspace codebase and system health...", "gemma-2b-it", 1, "find_by_name", {"pattern": "*.py"}),
        ("Synthesizing unified AST across 93 repositories...", "gemini-2.5-pro", 2, "ast_grep", {"rule": "pattern_match"}),
        ("Emitting verified deployment report to Cockpit...", "claude-3-7-sonnet", 3, "write_file", {"target": "status.json"}),
    ]

    for thought, model, step_num, tool, args in steps:
        print(f"  [Step {step_num}] Model: {model} -> Thought: '{thought}'")
        await client.publish_thought(thought, model=model, turn=step_num)
        await asyncio.sleep(0.4)
        print(f"  [Step {step_num}] Invoking Tool: {tool}...")
        await client.publish_tool_call(tool, args, duration_ms=45.2)
        await asyncio.sleep(0.4)
        await client.publish_step(step_num, f"Completed action: {tool}", status="completed")
        await asyncio.sleep(0.2)

    await client.stop()
    print("✨ Demo simulation completed.")


def main():
    parser = argparse.ArgumentParser(description="Microgravity Master Mesh Client CLI")
    parser.add_argument("--thought", type=str, help="Emit an agent thought")
    parser.add_argument("--model", type=str, default="gemini-2.5-pro", help="Model name")
    parser.add_argument("--step", type=int, default=1, help="Step index")
    parser.add_argument("--tool", type=str, help="Emit a tool call event")
    parser.add_argument("--args", type=str, default="{}", help="Tool args JSON string")
    parser.add_argument("--port", type=int, default=8765, help="EventBus port")
    parser.add_argument("--demo", action="store_true", help="Run simulated streaming agent turn demo")
    args = parser.parse_args()

    client = MicrogravityMeshClient()

    if args.demo:
        asyncio.run(run_demo_simulation(client))
        return

    if args.thought:
        ok = emit_thought_sync(args.thought, model=args.model, turn=args.step, port=args.port)
        print(f"Thought emitted: {'SUCCESS' if ok else 'OFFLINE_DROPPED'}")

    if args.tool:
        try:
            parsed_args = json.loads(args.args)
        except Exception:
            parsed_args = {"raw": args.args}
        ok = emit_tool_call_sync(args.tool, parsed_args, port=args.port)
        print(f"Tool call emitted: {'SUCCESS' if ok else 'OFFLINE_DROPPED'}")


if __name__ == "__main__":
    main()
