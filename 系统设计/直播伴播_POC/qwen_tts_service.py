import base64
import io
import os
import sys
import threading
import time
import wave

import dashscope
from dashscope.audio.qwen_tts_realtime import QwenTtsRealtime, QwenTtsRealtimeCallback, AudioFormat
from dashscope.audio.tts_v2 import SpeechSynthesizer as AudioSpeechSynthesizer, AudioFormat as AudioV2Format

MODEL = "qwen3-tts-instruct-flash-realtime"
VC_MODEL = "qwen3-tts-vc-realtime-2026-01-15"
PLUS_MODEL = "qwen-audio-3.0-tts-plus"
FLASH_MODEL = "qwen-audio-3.0-tts-flash"
VC_VOICE_PREFIX = "qwen-tts-vc-"
PLUS_VOICE_PREFIX = "qwen-audio-3.0-tts-plus-"
FLASH_VOICE_PREFIX = "qwen-audio-3.0-tts-flash-"
URL = "wss://dashscope.aliyuncs.com/api-ws/v1/realtime"
PRICE_PER_10K_CHARS_CNY = 1.0
PLUS_PRICE_PER_10K_CHARS_CNY = 1.4
FLASH_PRICE_PER_10K_CHARS_CNY = 1.0
DEFAULT_INSTRUCTIONS = (
    "直播间自然口播，语气连贯，句中停顿短，不要一小句一停；"
    "信息之间用自然衔接，节奏稍快但清晰，像真人场控连续讲话。"
)


def get_api_key():
    value = os.getenv("DASHSCOPE_API_KEY")
    if value:
        return value
    if sys.platform == "win32":
        import winreg
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, r"Environment") as key:
            value, _ = winreg.QueryValueEx(key, "DASHSCOPE_API_KEY")
            return value
    raise RuntimeError("DASHSCOPE_API_KEY not found")


class _Collector(QwenTtsRealtimeCallback):
    def __init__(self):
        super().__init__()
        self.done = threading.Event()
        self.audio = bytearray()
        self.error = None
        self.first_audio_at = None

    def on_open(self):
        return

    def on_close(self, close_status_code, close_msg):
        if not self.done.is_set():
            self.done.set()

    def on_event(self, response):
        try:
            event_type = response.get("type")
            if event_type == "response.audio.delta":
                if self.first_audio_at is None:
                    self.first_audio_at = time.perf_counter()
                self.audio.extend(base64.b64decode(response["delta"]))
            elif event_type == "session.finished":
                self.done.set()
            elif event_type == "error":
                self.error = response
                self.done.set()
        except Exception as exc:
            self.error = str(exc)
            self.done.set()


def synthesize_wav(
    text,
    voice="Cherry",
    speech_rate=1.08,
    instructions=DEFAULT_INSTRUCTIONS,
):
    text = str(text or "").strip()
    if not text:
        raise ValueError("TTS text is empty")
    if len(text) > 1000:
        raise ValueError("TTS text too long")

    dashscope.api_key = get_api_key()
    voice = str(voice or "").strip()
    is_plus_voice = voice.startswith(PLUS_VOICE_PREFIX)
    is_flash_voice = voice.startswith(FLASH_VOICE_PREFIX)

    # Qwen-Audio 3.0 TTS family: cloned voice + instruction control.
    if is_plus_voice or is_flash_voice:
        active_audio_model = PLUS_MODEL if is_plus_voice else FLASH_MODEL
        price_per_10k = PLUS_PRICE_PER_10K_CHARS_CNY if is_plus_voice else FLASH_PRICE_PER_10K_CHARS_CNY

        started = time.perf_counter()
        synthesizer = AudioSpeechSynthesizer(
            model=active_audio_model,
            voice=voice,
            format=AudioV2Format.WAV_24000HZ_MONO_16BIT,
            speech_rate=float(speech_rate),
            instruction=str(instructions or DEFAULT_INSTRUCTIONS),
            language_hints=["zh"],
        )
        wav_bytes = synthesizer.call(text, timeout_millis=45000)
        total_ms = (time.perf_counter() - started) * 1000
        if not wav_bytes:
            raise RuntimeError(active_audio_model + " returned no audio")
        try:
            first_ms = float(synthesizer.get_first_package_delay())
        except Exception:
            first_ms = total_ms

        chars = len(text)
        return {
            "wav": wav_bytes,
            "model": active_audio_model,
            "voice": voice,
            "speech_rate": float(speech_rate),
            "chars": chars,
            "first_audio_ms": round(first_ms, 1),
            "total_ms": round(total_ms, 1),
            "estimated_cost_cny": round(chars / 10000.0 * price_per_10k, 6),
        }

    # Existing Qwen3-TTS realtime path.
    callback = _Collector()
    is_cloned_voice = voice.startswith(VC_VOICE_PREFIX)
    active_model = VC_MODEL if is_cloned_voice else MODEL
    client = QwenTtsRealtime(model=active_model, callback=callback, url=URL)

    started = time.perf_counter()
    try:
        client.connect()
        session_args = {
            "voice": voice,
            "response_format": AudioFormat.PCM_24000HZ_MONO_16BIT,
            "mode": "server_commit",
            "language_type": "Chinese",
            "speech_rate": float(speech_rate),
        }
        if not is_cloned_voice:
            session_args["instructions"] = instructions
            session_args["optimize_instructions"] = False
        client.update_session(**session_args)
        client.append_text(text)
        client.finish()

        if not callback.done.wait(30):
            raise TimeoutError("Qwen TTS timeout")

        total_ms = (time.perf_counter() - started) * 1000
        first_ms = total_ms if callback.first_audio_at is None else (callback.first_audio_at - started) * 1000
        if callback.error:
            raise RuntimeError(str(callback.error))
        if not callback.audio:
            raise RuntimeError("Qwen TTS returned no audio")

        buffer = io.BytesIO()
        with wave.open(buffer, "wb") as wf:
            wf.setnchannels(1)
            wf.setsampwidth(2)
            wf.setframerate(24000)
            wf.writeframes(bytes(callback.audio))

        chars = len(text)
        return {
            "wav": buffer.getvalue(),
            "model": active_model,
            "voice": voice,
            "speech_rate": float(speech_rate),
            "chars": chars,
            "first_audio_ms": round(first_ms, 1),
            "total_ms": round(total_ms, 1),
            "estimated_cost_cny": round(chars / 10000.0 * PRICE_PER_10K_CHARS_CNY, 6),
        }
    finally:
        try:
            client.close()
        except Exception:
            pass

