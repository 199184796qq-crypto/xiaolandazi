# -*- coding: utf-8 -*-
from __future__ import annotations

from collections import deque
from dataclasses import dataclass, field
from typing import Any, Deque, Dict, List, Optional, Tuple


@dataclass
class Metrics:
    raw_event_count: int = 0
    rule_fast_path_count: int = 0
    monitor_agent_call_count: int = 0
    control_agent_call_count: int = 0
    monitor_input_tokens: int = 0
    monitor_output_tokens: int = 0
    control_input_tokens: int = 0
    control_output_tokens: int = 0
    template_hit_count: int = 0
    text_cache_hit_count: int = 0
    soft_interrupt_count: int = 0
    hard_interrupt_count: int = 0
    stale_speech_drop_count: int = 0
    expired_speech_drop_count: int = 0
    resume_count: int = 0
    resume_success_count: int = 0
    resume_bridge_count: int = 0
    resume_abandon_count: int = 0
    mid_sentence_resume_count: int = 0


@dataclass
class RoomState:
    room_id: str
    now_ms: int = 0
    current_product_id: str = "duck_001"
    product_context_version: int = 1
    room_context_version: int = 1
    sales_stage: str = "VALUE_BUILDING"
    online_count: int = 18
    recent_add_cart_count: int = 0
    recent_order_count: int = 0
    anchor_speaking: bool = False
    status: str = "IDLE"
    last_ai_spoken_at: int = 0
    last_sales_push_at: int = -999_999
    last_welcome_at: int = -999_999
    current_speech_task_id: Optional[str] = None
    recent_semantic_tags: Deque[str] = field(default_factory=lambda: deque(maxlen=8))

    def tick(self, ms: int) -> None:
        self.now_ms += ms


@dataclass
class RoomEvent:
    event_id: str
    type: str
    payload: Dict[str, Any] = field(default_factory=dict)
    ttl_ms: int = 5_000
    created_ms: int = 0


@dataclass
class Action:
    action: str
    priority: int
    interrupt_policy: str
    generation_mode: str
    channel: str = "VOICE"
    ttl_ms: int = 5_000
    reason_code: str = ""
    payload: Dict[str, Any] = field(default_factory=dict)


@dataclass
class SpeechTask:
    task_id: str
    action: str
    priority: int
    interrupt_policy: str
    resume_policy: str
    segments: List[str]
    source_mode: str
    created_ms: int
    expires_at: int
    product_id: str
    product_context_version: int
    room_context_version: int
    resume_hint: str = "DIRECT"
    current_segment_index: int = 0

    @property
    def current_segment(self) -> str:
        return self.segments[self.current_segment_index]


class Logger:
    def __init__(self) -> None:
        self.lines: List[str] = []

    def log(self, state: RoomState, message: str) -> None:
        line = f"[{state.now_ms:05d}ms] {message}"
        self.lines.append(line)
        print(line)


class FakeMonitorAgent:
    """POC only: deterministic stand-in for a real Bailian Monitor Agent."""

    def __init__(self, metrics: Metrics, logger: Logger) -> None:
        self.metrics = metrics
        self.logger = logger

    def decide(self, state: RoomState, event: RoomEvent) -> Action:
        self.metrics.monitor_agent_call_count += 1
        self.metrics.monitor_input_tokens += 120
        self.metrics.monitor_output_tokens += 18
        self.logger.log(state, f"Monitor Agent called for {event.type}")

        if event.type == "COMMENT":
            text = str(event.payload.get("text", ""))
            return Action(
                action="ANSWER_COMMENT",
                priority=1,
                interrupt_policy="SOFT_INTERRUPT",
                generation_mode="AGENT_ALLOWED",
                ttl_ms=8_000,
                reason_code="COMPLEX_USER_QUESTION",
                payload={"question": text, "intent": "COMPLEX_QUERY"},
            )

        if event.type == "TIMER_SALES_PUSH_DUE":
            if state.recent_add_cart_count >= 8 and state.recent_order_count <= 2:
                return Action(
                    action="SALES_PUSH",
                    priority=3,
                    interrupt_policy="SOFT_INTERRUPT",
                    generation_mode="TEMPLATE_FIRST",
                    ttl_ms=6_000,
                    reason_code="HIGH_CART_LOW_ORDER",
                    payload={"level": 1},
                )
            return Action(
                action="NO_ACTION",
                priority=9,
                interrupt_policy="DROP_IF_BUSY",
                generation_mode="NONE",
                reason_code="SALES_PUSH_NOT_NEEDED",
            )

        return Action(
            action="NO_ACTION",
            priority=9,
            interrupt_policy="DROP_IF_BUSY",
            generation_mode="NONE",
            reason_code="UNKNOWN_EVENT",
        )


class EventEngine:
    def __init__(self, metrics: Metrics, monitor_agent: FakeMonitorAgent, logger: Logger) -> None:
        self.metrics = metrics
        self.monitor_agent = monitor_agent
        self.logger = logger
        self._seen_event_ids: set[str] = set()

    def decide(self, state: RoomState, event: RoomEvent) -> Optional[Action]:
        self.metrics.raw_event_count += 1

        if event.event_id in self._seen_event_ids:
            self.metrics.rule_fast_path_count += 1
            self.logger.log(state, f"Event {event.event_id} deduped -> NO_ACTION (0 Token)")
            return None
        self._seen_event_ids.add(event.event_id)

        if state.now_ms - event.created_ms > event.ttl_ms:
            self.metrics.rule_fast_path_count += 1
            self.logger.log(state, f"Event {event.event_id} expired -> NO_ACTION (0 Token)")
            return None

        if event.type == "USER_ENTER":
            self.metrics.rule_fast_path_count += 1
            if event.payload.get("vip") or event.payload.get("returning"):
                return Action(
                    action="WELCOME_USER",
                    priority=4,
                    interrupt_policy="DROP_IF_BUSY",
                    generation_mode="TEMPLATE_FIRST",
                    ttl_ms=4_000,
                    reason_code="VALUABLE_USER_ENTER",
                    payload={"nickname": event.payload.get("nickname", "朋友")},
                )
            self.logger.log(state, "Normal user enter -> NO_ACTION (0 Token)")
            return None

        if event.type == "LIKE":
            self.metrics.rule_fast_path_count += 1
            if int(event.payload.get("batch_count", 0)) < 100:
                self.logger.log(state, "Like batch below threshold -> NO_ACTION (0 Token)")
                return None
            return Action(
                action="LIKE_REMINDER",
                priority=5,
                interrupt_policy="DROP_IF_BUSY",
                generation_mode="TEMPLATE_FIRST",
                ttl_ms=3_000,
                reason_code="LIKE_MILESTONE",
            )

        if event.type == "COMMENT":
            text = str(event.payload.get("text", "")).strip()
            normalized = text.replace(" ", "")
            if any(key in normalized for key in ("多少钱", "价格", "几块", "啥价")):
                self.metrics.rule_fast_path_count += 1
                self.logger.log(state, "Price intent matched by rule -> ANSWER_COMMENT (0 Monitor Token)")
                return Action(
                    action="ANSWER_COMMENT",
                    priority=1,
                    interrupt_policy="SOFT_INTERRUPT",
                    generation_mode="TEMPLATE_FIRST",
                    ttl_ms=8_000,
                    reason_code="PRICE_QUERY_RULE",
                    payload={"intent": "PRICE_QUERY", "question": text},
                )
            return self.monitor_agent.decide(state, event)

        if event.type == "TIMER_SALES_PUSH_DUE":
            return self.monitor_agent.decide(state, event)

        self.metrics.rule_fast_path_count += 1
        self.logger.log(state, f"{event.type} handled by rule -> NO_ACTION (0 Token)")
        return None


class FakeControlAgent:
    """POC only: deterministic stand-in for a real Bailian Control Agent."""

    def __init__(self, metrics: Metrics, logger: Logger) -> None:
        self.metrics = metrics
        self.logger = logger

    def generate(self, state: RoomState, action: Action) -> Tuple[str, str]:
        self.metrics.control_agent_call_count += 1
        self.metrics.control_input_tokens += 180
        self.metrics.control_output_tokens += 35
        self.logger.log(state, f"Control Agent called for {action.action}")

        question = str(action.payload.get("question", ""))
        if "老人" in question:
            return (
                "给家里老人吃的话，建议先看个人饮食习惯和配料信息，口感这边我可以继续给您介绍。",
                "AGENT",
            )
        return ("这个问题我给您简短说明一下，具体以当前商品页面和实际信息为准。", "AGENT")


class ControlOrchestrator:
    def __init__(self, metrics: Metrics, control_agent: FakeControlAgent, logger: Logger) -> None:
        self.metrics = metrics
        self.control_agent = control_agent
        self.logger = logger
        self._task_seq = 0
        self._text_cache: Dict[str, str] = {}

    def _next_task_id(self) -> str:
        self._task_seq += 1
        return f"sp_{self._task_seq:04d}"

    def build_task(self, state: RoomState, action: Action) -> Optional[SpeechTask]:
        if action.action == "NO_ACTION":
            return None

        text = ""
        source = ""
        resume_hint = "DIRECT"

        if action.action == "WELCOME_USER":
            self.metrics.template_hit_count += 1
            text = f"{action.payload.get('nickname', '朋友')}来了，欢迎哈，先慢慢看。"
            source = "TEMPLATE"

        elif action.action == "LIKE_REMINDER":
            self.metrics.template_hit_count += 1
            text = "谢谢大家的点赞，喜欢的朋友可以继续帮忙点一点。"
            source = "TEMPLATE"

        elif action.action == "ANSWER_COMMENT" and action.payload.get("intent") == "PRICE_QUERY":
            cache_key = f"{state.current_product_id}:PRICE_QUERY"
            if cache_key in self._text_cache:
                self.metrics.text_cache_hit_count += 1
                text = self._text_cache[cache_key]
                source = "TEXT_CACHE"
            else:
                self.metrics.template_hit_count += 1
                text = "这款现在直播间是六十九元，具体优惠以当前页面显示为准。"
                source = "TEMPLATE"
                self._text_cache[cache_key] = text

        elif action.action == "SALES_PUSH":
            self.metrics.template_hit_count += 1
            text = "已经加购的朋友可以看一下，下单按顺序安排，喜欢的话不用一直放在购物车里。"
            source = "TEMPLATE"
            resume_hint = "BRIDGE"

        else:
            text, source = self.control_agent.generate(state, action)
            resume_hint = "BRIDGE"

        self.logger.log(state, f"Speech text ready via {source}: {text}")

        return SpeechTask(
            task_id=self._next_task_id(),
            action=action.action,
            priority=action.priority,
            interrupt_policy=action.interrupt_policy,
            resume_policy="RESUME_MAIN",
            segments=[text],
            source_mode=source,
            created_ms=state.now_ms,
            expires_at=state.now_ms + action.ttl_ms,
            product_id=state.current_product_id,
            product_context_version=state.product_context_version,
            room_context_version=state.room_context_version,
            resume_hint=resume_hint,
        )


class AudioFocusManager:
    def __init__(self, state: RoomState, metrics: Metrics, logger: Logger) -> None:
        self.state = state
        self.metrics = metrics
        self.logger = logger
        self.current: Optional[SpeechTask] = None
        self.pending_interrupt: Optional[SpeechTask] = None
        self.queue: Deque[SpeechTask] = deque()
        self.suspended_main: Optional[Tuple[SpeechTask, int]] = None

    def _valid(self, task: SpeechTask) -> bool:
        if self.state.now_ms > task.expires_at:
            self.metrics.expired_speech_drop_count += 1
            self.logger.log(self.state, f"DROP expired task {task.task_id} ({task.action})")
            return False
        if task.product_context_version != self.state.product_context_version:
            self.metrics.stale_speech_drop_count += 1
            self.logger.log(
                self.state,
                f"DROP stale task {task.task_id}: product_version "
                f"{task.product_context_version}!={self.state.product_context_version}",
            )
            return False
        return True

    def start_main(self, segments: List[str]) -> None:
        task = SpeechTask(
            task_id="main_0001",
            action="MAIN",
            priority=6,
            interrupt_policy="SOFT_INTERRUPT",
            resume_policy="RESUME_MAIN",
            segments=segments,
            source_mode="OFFLINE_AUDIO",
            created_ms=self.state.now_ms,
            expires_at=self.state.now_ms + 3_600_000,
            product_id=self.state.current_product_id,
            product_context_version=self.state.product_context_version,
            room_context_version=self.state.room_context_version,
        )
        self._start(task)

    def _start(self, task: SpeechTask) -> None:
        if not self._valid(task):
            return
        self.current = task
        self.state.current_speech_task_id = task.task_id
        self.state.status = "PLAYING_MAIN" if task.action == "MAIN" else "PLAYING_INTERACTION"
        self.logger.log(
            self.state,
            f"PLAY {task.task_id}/{task.action} seg#{task.current_segment_index + 1}: "
            f"{task.current_segment}",
        )

    def submit(self, task: SpeechTask) -> None:
        if not self._valid(task):
            return

        if self.current is None:
            self._start(task)
            return

        if task.interrupt_policy == "DROP_IF_BUSY":
            self.logger.log(self.state, f"DROP_IF_BUSY {task.task_id}/{task.action}")
            return

        if task.interrupt_policy == "HARD_INTERRUPT":
            self.metrics.hard_interrupt_count += 1
            self.logger.log(self.state, f"HARD_INTERRUPT by {task.task_id}/{task.action}")
            if self.current.action == "MAIN":
                self.suspended_main = None
            self.current = None
            self.pending_interrupt = None
            self._start(task)
            return

        if task.interrupt_policy == "SOFT_INTERRUPT":
            if self.current.action == "MAIN":
                self.metrics.soft_interrupt_count += 1
                self.pending_interrupt = task
                self.state.status = "INTERRUPT_PENDING"
                self.logger.log(
                    self.state,
                    f"SOFT_INTERRUPT pending {task.task_id}; wait current main segment to finish",
                )
                return

            if task.priority < self.current.priority:
                self.metrics.soft_interrupt_count += 1
                self.logger.log(
                    self.state,
                    f"Interaction {task.task_id} queued ahead of lower priority current interaction",
                )
                self.queue.appendleft(task)
            else:
                self.queue.append(task)
            return

        self.queue.append(task)

    def finish_current_segment(self, elapsed_ms: int = 900) -> None:
        if self.current is None:
            return

        self.state.tick(elapsed_ms)
        finished = self.current
        self.logger.log(
            self.state,
            f"FINISH segment {finished.task_id}#{finished.current_segment_index + 1}",
        )

        if finished.action == "MAIN" and self.pending_interrupt is not None:
            next_index = finished.current_segment_index + 1
            if next_index < len(finished.segments):
                self.suspended_main = (finished, next_index)
                self.logger.log(
                    self.state,
                    f"Save resume point: {finished.task_id} next segment #{next_index + 1}",
                )
            else:
                self.suspended_main = None

            interrupt_task = self.pending_interrupt
            self.pending_interrupt = None
            self.current = None
            self._start(interrupt_task)
            return

        if finished.current_segment_index + 1 < len(finished.segments):
            finished.current_segment_index += 1
            self._start(finished)
            return

        self.logger.log(self.state, f"COMPLETE task {finished.task_id}/{finished.action}")
        self.current = None
        self.state.current_speech_task_id = None
        self.state.last_ai_spoken_at = self.state.now_ms

        if finished.action != "MAIN":
            self._recover_main(finished.resume_hint)
        elif self.queue:
            self._start(self.queue.popleft())
        else:
            self.state.status = "IDLE"

    def _recover_main(self, resume_hint: str) -> None:
        if self.suspended_main is None:
            if self.queue:
                self._start(self.queue.popleft())
            else:
                self.state.status = "IDLE"
            return

        main_task, next_index = self.suspended_main
        self.metrics.resume_count += 1

        if main_task.product_context_version != self.state.product_context_version:
            self.metrics.resume_abandon_count += 1
            self.logger.log(self.state, "ABANDON main resume: product context changed")
            self.suspended_main = None
            return

        if resume_hint == "BRIDGE":
            self.metrics.resume_bridge_count += 1
            self.logger.log(
                self.state,
                "BRIDGE(template, 0 Token): 好，刚才这个问题说清楚了，我们接着看这个产品本身。",
            )
            self.state.tick(450)

        main_task.current_segment_index = next_index
        self.suspended_main = None
        self.metrics.resume_success_count += 1
        self.logger.log(
            self.state,
            f"RESUME main from full segment #{next_index + 1} (never mid-sentence)",
        )
        self._start(main_task)

    def invalidate_for_product_switch(self, new_product_id: str) -> None:
        old_product = self.state.current_product_id
        self.state.current_product_id = new_product_id
        self.state.product_context_version += 1
        self.state.room_context_version += 1
        self.logger.log(
            self.state,
            f"PRODUCT_SWITCH {old_product} -> {new_product_id}; "
            f"product_version={self.state.product_context_version}",
        )

        if self.pending_interrupt is not None and not self._valid(self.pending_interrupt):
            self.pending_interrupt = None

        kept: Deque[SpeechTask] = deque()
        while self.queue:
            item = self.queue.popleft()
            if self._valid(item):
                kept.append(item)
        self.queue = kept

        if self.suspended_main is not None:
            main_task, _ = self.suspended_main
            if main_task.product_context_version != self.state.product_context_version:
                self.metrics.resume_abandon_count += 1
                self.logger.log(self.state, "Drop old suspended main after product switch")
                self.suspended_main = None

        if self.current is not None and self.current.product_context_version != self.state.product_context_version:
            self.metrics.hard_interrupt_count += 1
            self.logger.log(
                self.state,
                f"STOP stale current task {self.current.task_id}/{self.current.action} after product switch",
            )
            self.current = None
            self.state.current_speech_task_id = None
            self.state.status = "IDLE"


class PocRuntime:
    def __init__(self) -> None:
        self.metrics = Metrics()
        self.state = RoomState(room_id="room_001")
        self.logger = Logger()
        self.monitor_agent = FakeMonitorAgent(self.metrics, self.logger)
        self.event_engine = EventEngine(self.metrics, self.monitor_agent, self.logger)
        self.control_agent = FakeControlAgent(self.metrics, self.logger)
        self.control = ControlOrchestrator(self.metrics, self.control_agent, self.logger)
        self.focus = AudioFocusManager(self.state, self.metrics, self.logger)

    def emit(self, event: RoomEvent) -> Optional[SpeechTask]:
        if event.created_ms == 0:
            event.created_ms = self.state.now_ms
        self.logger.log(self.state, f"EVENT {event.event_id}/{event.type} {event.payload}")
        action = self.event_engine.decide(self.state, event)
        if action is None:
            return None
        self.logger.log(
            self.state,
            f"ACTION {action.action} priority={action.priority} "
            f"interrupt={action.interrupt_policy} reason={action.reason_code}",
        )
        task = self.control.build_task(self.state, action)
        if task is not None:
            self.focus.submit(task)
        return task

    def report(self) -> None:
        m = self.metrics
        print("\n=== POC METRICS ===")
        fields = [
            "raw_event_count",
            "rule_fast_path_count",
            "monitor_agent_call_count",
            "control_agent_call_count",
            "monitor_input_tokens",
            "monitor_output_tokens",
            "control_input_tokens",
            "control_output_tokens",
            "template_hit_count",
            "text_cache_hit_count",
            "soft_interrupt_count",
            "hard_interrupt_count",
            "stale_speech_drop_count",
            "expired_speech_drop_count",
            "resume_count",
            "resume_success_count",
            "resume_bridge_count",
            "resume_abandon_count",
            "mid_sentence_resume_count",
        ]
        for name in fields:
            print(f"{name}={getattr(m, name)}")

        total_tokens = (
            m.monitor_input_tokens
            + m.monitor_output_tokens
            + m.control_input_tokens
            + m.control_output_tokens
        )
        print(f"total_simulated_agent_tokens={total_tokens}")


def run_demo() -> None:
    rt = PocRuntime()

    print("=== SCENARIO 1: main line + low-value events (0 Token) ===")
    rt.focus.start_main(
        [
            "我们这个鸭子最大的特点，就是外皮吃起来比较酥。",
            "里面的肉不会特别柴，日常加热也比较方便。",
            "接下来我再给大家说一下现在直播间的价格和发货安排。",
        ]
    )

    rt.emit(RoomEvent("e001", "USER_ENTER", {"nickname": "小王", "vip": False}))
    rt.emit(RoomEvent("e002", "LIKE", {"batch_count": 36}))

    print("\n=== SCENARIO 2: price question -> rule/template -> soft interrupt -> direct resume ===")
    rt.emit(RoomEvent("e003", "COMMENT", {"text": "这个多少钱？"}, ttl_ms=8_000))
    rt.focus.finish_current_segment(1_000)
    rt.focus.finish_current_segment(750)

    print("\n=== SCENARIO 3: complex question -> Monitor Agent + Control Agent -> bridge resume ===")
    rt.emit(RoomEvent("e004", "COMMENT", {"text": "这个给家里老人吃合适吗？"}, ttl_ms=8_000))
    rt.focus.finish_current_segment(1_100)
    rt.focus.finish_current_segment(900)

    print("\n=== SCENARIO 4: sales push generated, then product switches before playback ===")
    rt.state.recent_add_cart_count = 12
    rt.state.recent_order_count = 2
    sales_task = rt.emit(RoomEvent("e005", "TIMER_SALES_PUSH_DUE", {}, ttl_ms=6_000))
    assert sales_task is not None
    rt.focus.invalidate_for_product_switch("duck_002")

    print("\n=== SCENARIO 5: duplicate event is suppressed ===")
    rt.emit(RoomEvent("e006", "USER_ENTER", {"nickname": "李姐", "vip": False}))
    rt.emit(RoomEvent("e006", "USER_ENTER", {"nickname": "李姐", "vip": False}))

    rt.report()

    m = rt.metrics
    assert m.raw_event_count == 7, m.raw_event_count
    assert m.monitor_agent_call_count == 2, m.monitor_agent_call_count
    assert m.control_agent_call_count == 1, m.control_agent_call_count
    assert m.soft_interrupt_count >= 2, m.soft_interrupt_count
    assert m.resume_success_count >= 2, m.resume_success_count
    assert m.resume_bridge_count >= 1, m.resume_bridge_count
    assert m.stale_speech_drop_count >= 1, m.stale_speech_drop_count
    assert m.mid_sentence_resume_count == 0
    assert m.rule_fast_path_count >= 4, m.rule_fast_path_count

    print("\nPOC_ASSERTIONS=PASS")


if __name__ == "__main__":
    run_demo()
