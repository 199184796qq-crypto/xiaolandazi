import base64
import io
import json
import os
import sys
import threading
import time
import wave

import dashscope
from dashscope.audio.qwen_tts_realtime import QwenTtsRealtime, QwenTtsRealtimeCallback, AudioFormat

MODEL = "qwen3-tts-flash-realtime"
VOICE = "Cherry"
URL = "wss://dashscope.aliyuncs.com/api-ws/v1/realtime"


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


class Collector(QwenTtsRealtimeCallback):
    def __init__(self):
        super().__init__()
        self.done = threading.Event()
        self.audio = bytearray()
        self.error = None
        self.first_audio_at = None

    def on_open(self):
        pass

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


def synthesize(text):
    dashscope.api_key = get_api_key()
    callback = Collector()
    client = QwenTtsRealtime(model=MODEL, callback=callback, url=URL)

    started = time.perf_counter()
    client.connect()
    client.update_session(
        voice=VOICE,
        response_format=AudioFormat.PCM_24000HZ_MONO_16BIT,
        mode="server_commit",
        language_type="Chinese",
        speech_rate=1.0,
    )
    client.append_text(text)
    client.finish()
    if not callback.done.wait(30):
        client.close()
        raise TimeoutError("TTS timeout")
    total_ms = (time.perf_counter() - started) * 1000
    first_ms = None if callback.first_audio_at is None else (callback.first_audio_at - started) * 1000
    if callback.error:
        raise RuntimeError(str(callback.error))

    out = os.path.join(os.path.dirname(__file__), "qwen_tts_poc.wav")
    with wave.open(out, "wb") as wf:
        wf.setnchannels(1)
        wf.setsampwidth(2)
        wf.setframerate(24000)
        wf.writeframes(bytes(callback.audio))

    chars = len(text)
    estimated_cost = chars / 10000.0 * 1.0
    print("QWEN_TTS_POC=PASS")
    print("model=" + MODEL)
    print("voice=" + VOICE)
    print("chars=" + str(chars))
    print("first_audio_ms=" + str(round(first_ms or total_ms, 1)))
    print("total_ms=" + str(round(total_ms, 1)))
    print("audio_bytes=" + str(len(callback.audio)))
    print("estimated_cost_cny=" + str(round(estimated_cost, 6)))
    print("wav=" + out)


if __name__ == "__main__":
    synthesize("1号童子鸡是生鲜，拿到手还是生的，回家需要自己烹饪。")