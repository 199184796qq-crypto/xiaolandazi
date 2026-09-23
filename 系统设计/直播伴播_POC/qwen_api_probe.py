import json, os, sys, time, requests

def key():
    v=os.getenv('DASHSCOPE_API_KEY')
    if v: return v
    if sys.platform=='win32':
        import winreg
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, r'Environment') as k:
            return winreg.QueryValueEx(k,'DASHSCOPE_API_KEY')[0]
    raise RuntimeError('no key')

url='https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions'
payload={'model':'qwen3.8-flash','messages':[{'role':'user','content':'Only reply OK'}],'enable_thinking':False,'max_tokens':8}
t=time.perf_counter()
r=requests.post(url,headers={'Authorization':'Bearer '+key(),'Content-Type':'application/json'},json=payload,timeout=20)
print('status='+str(r.status_code))
print('elapsed_ms='+str(round((time.perf_counter()-t)*1000,1)))
print(r.text[:1000])
