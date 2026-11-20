#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Unit tests for BoundedBackpressureQueue (Emergent Metric Tier 26).
Verifies:
1. High-water mark & low-water mark flow control state transitions.
2. Priority-based event shedding for low-priority telemetry without dropping critical world actions or user events.
3. Memory-boundedness with zero memory leaks under 10k event bursts.
4. Logging of 'Backpressure flow control verified: 0 dropped critical events'.
5. Prevention of unhandled QueueFull and MemoryError.
"""

import asyncio
import gc
import logging
import os
import sys
import unittest
from pathlib import Path

# Add python/eventbus to sys.path
root_dir = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(root_dir / "python" / "eventbus"))

from backpressure_queue import (
    BoundedBackpressureQueue,
    Event,
    EventPriority,
    ShedPolicy,
)


class TestBackpressurePriorityAndShedding(unittest.TestCase):
    """Verifies priority classification, ordering, and shedding mechanics."""

    def test_topic_priority_classification(self):
        self.assertEqual(Event.classify_topic_priority("world.action.move"), EventPriority.CRITICAL)
        self.assertEqual(Event.classify_topic_priority("user.input.prompt"), EventPriority.CRITICAL)
        self.assertEqual(Event.classify_topic_priority("agent.state.checkpoint"), EventPriority.HIGH)
        self.assertEqual(Event.classify_topic_priority("telemetry.tokens.throughput"), EventPriority.TELEMETRY)
        self.assertEqual(Event.classify_topic_priority("metrics.cpu.tick"), EventPriority.TELEMETRY)
        self.assertEqual(Event.classify_topic_priority("log.sync"), EventPriority.LOW)
        self.assertEqual(Event.classify_topic_priority("chat.message"), EventPriority.NORMAL)

    def test_priority_dequeue_order(self):
        """Higher priority events must be dequeued before lower priority events regardless of enqueue order."""
        queue = BoundedBackpressureQueue(maxsize=10)

        # Enqueue in reverse priority order
        queue.put_nowait("telemetry_data", priority=EventPriority.TELEMETRY)
        queue.put_nowait("low_log", priority=EventPriority.LOW)
        queue.put_nowait("normal_msg", priority=EventPriority.NORMAL)
        queue.put_nowait("high_state", priority=EventPriority.HIGH)
        queue.put_nowait("critical_action", priority=EventPriority.CRITICAL)

        self.assertEqual(queue.get_nowait().payload, "critical_action")
        self.assertEqual(queue.get_nowait().payload, "high_state")
        self.assertEqual(queue.get_nowait().payload, "normal_msg")
        self.assertEqual(queue.get_nowait().payload, "low_log")
        self.assertEqual(queue.get_nowait().payload, "telemetry_data")

    def test_low_priority_shedding_under_full_queue(self):
        """When queue reaches capacity, low-priority and telemetry events are shed, preserving critical ones."""
        shed_events = []
        queue = BoundedBackpressureQueue(
            maxsize=5,
            high_water_ratio=0.8,
            low_water_ratio=0.4,
            on_shed=lambda ev: shed_events.append(ev),
        )

        # Fill queue to capacity with NORMAL events
        for i in range(5):
            success = queue.put_nowait(f"normal_{i}", priority=EventPriority.NORMAL)
            self.assertTrue(success)

        self.assertTrue(queue.full())
        self.assertEqual(queue.qsize(), 5)

        # Try putting TELEMETRY event when full: must be shed
        shed_ok = queue.put_nowait("telemetry_shed", priority=EventPriority.TELEMETRY)
        self.assertFalse(shed_ok)
        self.assertEqual(len(shed_events), 1)
        self.assertEqual(shed_events[0].priority, EventPriority.TELEMETRY)

        # Try putting CRITICAL event: must succeed and evict a lower priority event
        crit_ok = queue.put_nowait("critical_action_1", priority=EventPriority.CRITICAL)
        self.assertTrue(crit_ok)
        self.assertEqual(queue.stats.dropped_critical, 0)
        self.assertEqual(len(shed_events), 2)  # One telemetry rejected, one normal evicted


class TestWatermarkFlowControl(unittest.IsolatedAsyncioTestCase):
    """Tests high-water mark and low-water mark state transitions and capacity pauses."""

    async def test_watermark_transitions(self):
        high_triggered = []
        low_triggered = []

        queue = BoundedBackpressureQueue(
            maxsize=10,
            high_water_ratio=0.7,   # High-water mark = 7
            low_water_ratio=0.3,    # Low-water mark = 3
            on_high_water=lambda q: high_triggered.append(q.qsize()),
            on_low_water=lambda q: low_triggered.append(q.qsize()),
        )

        self.assertFalse(queue.is_congested)

        # Enqueue 6 items (below high-water mark)
        for i in range(6):
            queue.put_nowait(f"item_{i}", priority=EventPriority.NORMAL)
        self.assertFalse(queue.is_congested)
        self.assertEqual(len(high_triggered), 0)

        # 7th item breaches high-water mark
        queue.put_nowait("item_6", priority=EventPriority.NORMAL)
        self.assertTrue(queue.is_congested)
        self.assertEqual(len(high_triggered), 1)
        self.assertEqual(high_triggered[0], 7)

        # Consume until below low-water mark
        queue.get_nowait()  # size = 6
        queue.get_nowait()  # size = 5
        queue.get_nowait()  # size = 4
        self.assertTrue(queue.is_congested)  # Still congested until <= 3

        queue.get_nowait()  # size = 3: restores low-water mark!
        self.assertFalse(queue.is_congested)
        self.assertEqual(len(low_triggered), 1)
        self.assertEqual(low_triggered[0], 3)

    async def test_wait_for_capacity_unblocks_on_low_water(self):
        queue = BoundedBackpressureQueue(
            maxsize=10,
            high_water_ratio=0.6,   # High-water mark = 6
            low_water_ratio=0.3,    # Low-water mark = 3
        )

        for i in range(7):
            queue.put_nowait(f"item_{i}", priority=EventPriority.NORMAL)
        self.assertTrue(queue.is_congested)

        unblocked = False

        async def producer():
            nonlocal unblocked
            await queue.wait_for_capacity()
            unblocked = True

        task = asyncio.create_task(producer())
        await asyncio.sleep(0.01)
        self.assertFalse(unblocked)

        # Drain to low water mark
        while queue.qsize() > 3:
            queue.get_nowait()

        await asyncio.wait_for(task, timeout=1.0)
        self.assertTrue(unblocked)
        self.assertFalse(queue.is_congested)


class TestBurstAndZeroCriticalDrops(unittest.IsolatedAsyncioTestCase):
    """
    Simulates high token throughput bursts (10k events) and verifies:
    1. Zero dropped critical events (Invariant requirement).
    2. Zero unhandled QueueFull exceptions.
    3. Zero MemoryError.
    4. Exact log emitted: 'Backpressure flow control verified: 0 dropped critical events'.
    """

    async def test_10k_event_burst_with_zero_critical_drops(self):
        queue = BoundedBackpressureQueue(
            maxsize=500,
            high_water_ratio=0.8,
            low_water_ratio=0.5,
        )

        TOTAL_EVENTS = 10000
        critical_count = 0
        telemetry_count = 0

        # Run 10k burst
        for i in range(TOTAL_EVENTS):
            # Every 10th event is critical world action / user input
            if i % 10 == 0:
                is_crit = True
                prio = EventPriority.CRITICAL
                topic = "world.action.execute"
                critical_count += 1
            else:
                is_crit = False
                prio = EventPriority.TELEMETRY
                topic = "telemetry.tokens.tick"
                telemetry_count += 1

            queue.put_nowait(
                item={"burst_index": i, "data": "x" * 64},
                priority=prio,
                topic=topic,
            )

        stats = queue.get_stats()

        # Invariant checks
        self.assertEqual(stats["dropped_critical"], 0, "CRITICAL INVARIANT VIOLATED: Dropped critical events > 0")
        self.assertGreater(stats["shed_total"], 0, "Must shed low-priority events under burst")
        self.assertGreater(stats["high_water_incidents"], 0)

        # Verify log output via verify_flow_control
        with self.assertLogs("MicrogravityBackpressure", level="INFO") as log_capture:
            result = queue.verify_flow_control()

        self.assertTrue(result["verified"])
        self.assertEqual(result["dropped_critical_events"], 0)
        expected_msg = "Backpressure flow control verified: 0 dropped critical events"
        self.assertTrue(
            any(expected_msg in record.getMessage() for record in log_capture.records),
            f"Expected log message '{expected_msg}' not found in logs",
        )

    def test_memory_bounded_zero_leak_under_10k_burst(self):
        """Ensures memory remains bounded and all temporary objects are reclaimed under 10k event burst."""
        gc.collect()
        initial_objects = len(gc.get_objects())

        queue = BoundedBackpressureQueue(maxsize=200)

        # Burst 10,000 events through the queue
        for i in range(10000):
            prio = EventPriority.CRITICAL if (i % 20 == 0) else EventPriority.TELEMETRY
            queue.put_nowait(f"payload_{i}", priority=prio)
            if i % 5 == 0 and not queue.empty():
                queue.get_nowait()

        # Queue size must be strictly bounded
        self.assertLessEqual(queue.qsize(), 1000)

        # Drain remainder
        while not queue.empty():
            queue.get_nowait()

        queue.clear()
        gc.collect()
        final_objects = len(gc.get_objects())

        # Object growth after draining must be negligible (< 150 bookkeeping objects)
        object_delta = final_objects - initial_objects
        self.assertLess(object_delta, 150, f"Potential memory leak detected: object delta {object_delta}")

    async def test_async_producer_consumer_pipeline(self):
        """Simulates asynchronous concurrent producer and consumer handling high burst."""
        queue = BoundedBackpressureQueue(maxsize=100)
        received_critical = []
        received_telemetry = []

        async def consumer():
            while True:
                try:
                    event = await queue.get(timeout=0.2)
                    if event.is_critical:
                        received_critical.append(event.payload)
                    else:
                        received_telemetry.append(event.payload)
                    queue.task_done()
                except (asyncio.TimeoutError, asyncio.QueueEmpty):
                    break

        consumer_task = asyncio.create_task(consumer())

        CRITICAL_ITEMS = 50
        TELEMETRY_ITEMS = 1000

        # Producer bursts 1,050 events
        for i in range(CRITICAL_ITEMS):
            queue.put_nowait(f"critical_{i}", priority=EventPriority.CRITICAL)
        for i in range(TELEMETRY_ITEMS):
            queue.put_nowait(f"telemetry_{i}", priority=EventPriority.TELEMETRY)

        await consumer_task

        # All critical items must be received
        self.assertEqual(len(received_critical), CRITICAL_ITEMS)
        self.assertEqual(queue.stats.dropped_critical, 0)
        verification = queue.verify_flow_control()
        self.assertTrue(verification["verified"])


if __name__ == "__main__":
    unittest.main()
