#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Microgravity Agent EventBus Client
High-level async client enabling Antigravity / Microgravity AI agents
to subscribe to and publish telemetry, world actions, and simulation events.
"""

import sys
import json
import time
import asyncio
import inspect
import logging
import websockets
from typing import Dict, Any, Optional, Callable, List, Set

logger = logging.getLogger("MicrogravityEventBus")

class MicrogravityEventBusClient:
    def __init__(self, agent_id: str, server_url: str = "ws://127.0.0.1:8765"):
        self.agent_id = agent_id
        self.server_url = server_url
        self.subscriptions: Set[str] = set()
        self.handlers: Dict[str, List[Callable[[Dict[str, Any]], None]]] = {}
        self.is_connected = False
        self._connected_event = asyncio.Event()
        self._ws = None
        self._supervisor_task: Optional[asyncio.Task] = None
        self._running = False

    async def wait_until_connected(self, timeout: float = 5.0) -> bool:
        try:
            await asyncio.wait_for(self._connected_event.wait(), timeout=timeout)
            return True
        except asyncio.TimeoutError:
            return self.is_connected

    def on(self, topic_pattern: str):
        """Decorator for registering an event handler."""
        def decorator(func: Callable[[Dict[str, Any]], None]):
            self.subscribe(topic_pattern, func)
            return func
        return decorator

    def subscribe(self, topic_pattern: str, handler: Optional[Callable[[Dict[str, Any]], None]] = None):
        self.subscriptions.add(topic_pattern)
        if handler:
            if topic_pattern not in self.handlers:
                self.handlers[topic_pattern] = []
            self.handlers[topic_pattern].append(handler)
        if self.is_connected and self._ws:
            asyncio.create_task(self._send_subscribe([topic_pattern]))

    async def start(self, wait_connected: bool = True, timeout: float = 5.0):
        """Starts the client supervisor task in the current event loop."""
        self._running = True
        self._connected_event.clear()
        self._supervisor_task = asyncio.create_task(self._supervisor_loop())
        if wait_connected:
            await self.wait_until_connected(timeout=timeout)

    async def stop(self):
        self._running = False
        if self._ws:
            try:
                await asyncio.wait_for(self._ws.close(), timeout=1.0)
            except Exception:
                pass
            self._ws = None
        if self._supervisor_task:
            self._supervisor_task.cancel()
            try:
                await asyncio.wait_for(self._supervisor_task, timeout=1.0)
            except (asyncio.CancelledError, asyncio.TimeoutError, Exception):
                pass
            self._supervisor_task = None
        self.is_connected = False

    async def publish(self, topic: str, payload: Dict[str, Any]):
        """Publishes an event to the spine."""
        if not self.is_connected or not self._ws:
            logger.warning(f"Agent {self.agent_id} cannot publish: not connected to EventBus Spine.")
            return

        req = {
            "jsonrpc": "2.0",
            "id": f"pub_{int(time.time()*1000)}",
            "method": "eventbus.publish",
            "params": {
                "topic": topic,
                "source": self.agent_id,
                "timestamp": time.time(),
                "payload": payload
            }
        }
        await self._ws.send(json.dumps(req))

    async def _send_subscribe(self, topics: List[str]):
        if self._ws:
            req = {
                "jsonrpc": "2.0",
                "id": f"sub_{int(time.time()*1000)}",
                "method": "eventbus.subscribe",
                "params": {"topics": topics}
            }
            await self._ws.send(json.dumps(req))

    async def _supervisor_loop(self):
        import inspect
        while self._running:
            try:
                async with websockets.connect(
                    self.server_url,
                    ping_interval=None,
                    close_timeout=1.0
                ) as ws:
                    self._ws = ws
                    self.is_connected = True
                    
                    # Re-subscribe to all topics
                    if self.subscriptions:
                        await self._send_subscribe(list(self.subscriptions))

                    self._connected_event.set()

                    while self._running:
                        try:
                            raw_msg = await ws.recv()
                        except (websockets.ConnectionClosed, asyncio.CancelledError):
                            break
                        except Exception as e:
                            if not self._running:
                                break
                            logger.error(f"WebSocket receive error: {e}")
                            break

                        try:
                            data = json.loads(raw_msg)
                            if data.get("method") == "eventbus.event":
                                params = data.get("params", {})
                                topic = params.get("topic", "")
                                payload = params.get("payload", {})
                                for pattern, handlers in list(self.handlers.items()):
                                    if self._topic_matches(pattern, topic):
                                        for h in handlers:
                                            try:
                                                if inspect.iscoroutinefunction(h):
                                                    asyncio.create_task(h(payload))
                                                else:
                                                    h(payload)
                                            except Exception as exc:
                                                logger.error(f"Handler error for {pattern}: {exc}")
                        except Exception as e:
                            logger.error(f"Event decode error: {e}")
            except (websockets.ConnectionClosed, OSError) as e:
                self.is_connected = False
                self._connected_event.clear()
                self._ws = None
                if self._running:
                    await asyncio.sleep(0.5)
            except asyncio.CancelledError:
                break
            finally:
                self.is_connected = False
                self._connected_event.clear()
                self._ws = None

    def _dispatch_event(self, event_params: Dict[str, Any]):
        topic = event_params.get("topic", "")
        payload = event_params.get("payload", {})
        
        for pattern, handlers in self.handlers.items():
            if self._topic_matches(pattern, topic):
                for h in handlers:
                    try:
                        if inspect.iscoroutinefunction(h):
                            asyncio.create_task(h(payload))
                        else:
                            h(payload)
                    except Exception as exc:
                        logger.error(f"Handler for {pattern} raised error: {exc}")

    def _topic_matches(self, pattern: str, topic: str) -> bool:
        if pattern == topic or pattern == "**" or pattern == "#":
            return True
        if pattern.endswith(".**") or pattern.endswith(".#"):
            prefix = pattern[:-3]
            return topic == prefix or topic.startswith(prefix + ".")
        if pattern.endswith(".*"):
            prefix = pattern[:-2]
            return topic == prefix or topic.startswith(prefix + ".")
        p_segs = pattern.split(".")
        t_segs = topic.split(".")
        if len(p_segs) != len(t_segs):
            return False
        for p, t in zip(p_segs, t_segs):
            if p != "*" and p != t:
                return False
        return True
