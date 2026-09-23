import base64, os, sys, threading, time, wave
import dashscope
from dashscope.audio.qwen_tts_realtime import QwenTtsRealtime,QwenTtsRealtimeCallback,AudioFormat

def key():
    v=os.getenv("DASHSCOPE_API_KEY")
    if v:return v
    import winreg
    with winreg.OpenKey(winreg.HKEY_CURRENT_USER,r"Environment") as k:
        return winreg.QueryValueEx(k,"DASHSCOPE_API_KEY")[0]

class C(QwenTtsRealtimeCallback):
    def __init__(self):
        self.done=threading.Event(); self.audio=bytearray(); self.first=None; self.err=None
    def on_open(self): pass
    def on_close(self,a,b): self.done.set()
    def on_event(self,r):
        try:
            if r.get("type")=="response.audio.delta":
                if self.first is None:self.first=time.perf_counter()
                self.audio.extend(base64.b64decode(r["delta"]))
            elif r.get("type")=="session.finished": self.done.set()
            elif r.get("type")=="error": self.err=r; self.done.set()
        except Exception as e:self.err=e; self.done.set()

dashscope.api_key=key()
c=C(); t=time.perf_counter()
q=QwenTtsRealtime(model="qwen3-tts-instruct-flash-realtime",callback=c,url="wss://dashscope.aliyuncs.com/api-ws/v1/realtime")
q.connect()
q.update_session(voice="Cherry",response_format=AudioFormat.PCM_24000HZ_MONO_16BIT,mode="server_commit",language_type="Chinese",speech_rate=1.05,instructions="直播间自然口播，语气连贯，句中停顿短，不要一小句一停，信息之间自然衔接，节奏稍快但清晰。",optimize_instructions=True)
q.append_text("1号童子鸡是4到5个月没开叫的子公鸡，属于生鲜，单只净重1.2斤到1.8斤左右，回家需要自己烹饪。")
q.finish(); c.done.wait(30)
print("err="+str(c.err)); print("first_ms="+str(round(((c.first or time.perf_counter())-t)*1000,1))); print("bytes="+str(len(c.audio)))