import { createInterface } from "node:readline";
import { existsSync } from "node:fs";
import { createHash, randomBytes } from "node:crypto";
import tls from "node:tls";
import { chromium } from "playwright-core";

const DEFAULT_USER_AGENT =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36";
const DIRECT_WS_BOOTSTRAP_TIMEOUT_MS = 8000;
const MAX_BOOTSTRAP_BROWSER_FRAMES = 32;
const MAX_BOOTSTRAP_CLIENT_FRAMES = 1;
const DIRECT_WS_HEARTBEAT_MS = 10000;

const rooms = new Map();
let browser = null;
let browserContext = null;
let browserPromise = null;

function send(message) {
  process.stdout.write(JSON.stringify(message) + "\n");
}

function log(message) {
  process.stderr.write("[collector-worker] " + message + "\n");
}

function detectChrome() {
  const configured = process.env.COLLECTOR_BROWSER_PATH?.trim();
  const candidates = [
    configured,
    "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
    "C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe",
    process.env.LOCALAPPDATA
      ? process.env.LOCALAPPDATA + "\\Google\\Chrome\\Application\\chrome.exe"
      : "",
    "C:\\Program Files\\Microsoft\\Edge\\Application\\msedge.exe",
    "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe",
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    "/Applications/Chromium.app/Contents/MacOS/Chromium",
    "/usr/bin/google-chrome",
    "/usr/bin/google-chrome-stable",
    "/usr/bin/chromium",
    "/usr/bin/chromium-browser",
  ].filter(Boolean);

  return candidates.find((candidate) => {
    try {
      return existsSync(candidate);
    } catch {
      return false;
    }
  });
}

function isHeadless() {
  const raw = process.env.COLLECTOR_BROWSER_HEADLESS?.trim().toLowerCase();
  if (!raw) return true;
  return !["0", "false", "no", "off"].includes(raw);
}

function isDirectWebSocketEnabled() {
  const raw = process.env.COLLECTOR_DIRECT_WS?.trim().toLowerCase();
  if (!raw) return true;
  return !["0", "false", "no", "off"].includes(raw);
}

function headerValue(headers, name) {
  const lowerName = String(name).toLowerCase();
  for (const [key, value] of Object.entries(headers ?? {})) {
    if (String(key).toLowerCase() === lowerName) {
      return String(value ?? "");
    }
  }
  return "";
}

function cookieHeaderForURL(cookies, targetURL) {
  let hostname = "";
  try {
    hostname = new URL(targetURL).hostname.toLowerCase();
  } catch {
    return "";
  }

  return (cookies ?? [])
    .filter((cookie) => {
      const domain = String(cookie.domain ?? "")
        .replace(/^\./, "")
        .toLowerCase();
      return (
        domain &&
        (hostname === domain || hostname.endsWith("." + domain))
      );
    })
    .map((cookie) => cookie.name + "=" + cookie.value)
    .join("; ");
}

function encodeClientWebSocketFrame(opcode, payload) {
  const body = Buffer.isBuffer(payload) ? payload : Buffer.from(payload ?? "");
  const mask = randomBytes(4);
  let header;

  if (body.length < 126) {
    header = Buffer.alloc(2);
    header[0] = 0x80 | (opcode & 0x0f);
    header[1] = 0x80 | body.length;
  } else if (body.length < 65536) {
    header = Buffer.alloc(4);
    header[0] = 0x80 | (opcode & 0x0f);
    header[1] = 0x80 | 126;
    header.writeUInt16BE(body.length, 2);
  } else {
    header = Buffer.alloc(10);
    header[0] = 0x80 | (opcode & 0x0f);
    header[1] = 0x80 | 127;
    header.writeBigUInt64BE(BigInt(body.length), 2);
  }

  const masked = Buffer.alloc(body.length);
  for (let index = 0; index < body.length; index += 1) {
    masked[index] = body[index] ^ mask[index % 4];
  }
  return Buffer.concat([header, mask, masked]);
}

function cdpFramePayload(frame) {
  const opcode = Number(frame?.opcode ?? 0);
  const payloadData = String(frame?.payloadData ?? "");
  if (!payloadData) return null;

  if (opcode === 2) {
    try {
      return { opcode, payload: Buffer.from(payloadData, "base64") };
    } catch {
      return null;
    }
  }
  if (opcode === 1) {
    return { opcode, payload: Buffer.from(payloadData, "utf8") };
  }
  return null;
}

function readProtoVarint(data, startOffset) {
  let offset = startOffset;
  let value = 0n;
  let shift = 0n;
  while (offset < data.length && shift <= 63n) {
    const byte = data[offset];
    offset += 1;
    value |= BigInt(byte & 0x7f) << shift;
    if ((byte & 0x80) === 0) {
      return { value, offset };
    }
    shift += 7n;
  }
  return null;
}

function parseProtoFields(data) {
  const fields = [];
  let offset = 0;
  while (offset < data.length) {
    const key = readProtoVarint(data, offset);
    if (!key) break;
    offset = key.offset;
    const number = Number(key.value >> 3n);
    const wire = Number(key.value & 0x07n);

    if (wire === 0) {
      const value = readProtoVarint(data, offset);
      if (!value) break;
      fields.push({ number, wire, value: value.value });
      offset = value.offset;
      continue;
    }
    if (wire === 1) {
      if (offset + 8 > data.length) break;
      fields.push({ number, wire, bytes: data.subarray(offset, offset + 8) });
      offset += 8;
      continue;
    }
    if (wire === 2) {
      const lengthValue = readProtoVarint(data, offset);
      if (!lengthValue) break;
      offset = lengthValue.offset;
      const length = Number(lengthValue.value);
      if (!Number.isSafeInteger(length) || offset + length > data.length) break;
      fields.push({
        number,
        wire,
        bytes: data.subarray(offset, offset + length),
      });
      offset += length;
      continue;
    }
    if (wire === 5) {
      if (offset + 4 > data.length) break;
      fields.push({ number, wire, bytes: data.subarray(offset, offset + 4) });
      offset += 4;
      continue;
    }
    break;
  }
  return fields;
}

function encodeProtoVarint(value) {
  let remaining = BigInt(value);
  const bytes = [];
  while (remaining >= 0x80n) {
    bytes.push(Number((remaining & 0x7fn) | 0x80n));
    remaining >>= 7n;
  }
  bytes.push(Number(remaining));
  return Buffer.from(bytes);
}

function protoVarintField(number, value) {
  return Buffer.concat([
    encodeProtoVarint(BigInt(number << 3)),
    encodeProtoVarint(value),
  ]);
}

function protoBytesField(number, value) {
  const bytes = Buffer.isBuffer(value) ? value : Buffer.from(value ?? "");
  return Buffer.concat([
    encodeProtoVarint(BigInt((number << 3) | 2)),
    encodeProtoVarint(BigInt(bytes.length)),
    bytes,
  ]);
}

function buildDouyinAck(pushPayload) {
  const fields = parseProtoFields(pushPayload);
  const sequence = fields.find(
    (field) => field.number === 2 && field.wire === 0,
  )?.value;
  if (sequence == null) return null;

  let internalExt = null;
  for (const field of fields) {
    if (field.number !== 5 || field.wire !== 2 || !field.bytes) continue;
    const headerFields = parseProtoFields(field.bytes);
    const key = headerFields.find(
      (header) => header.number === 1 && header.wire === 2,
    )?.bytes;
    if (!key || key.toString("utf8").toLowerCase() !== "im-internal_ext") {
      continue;
    }
    internalExt = headerFields.find(
      (header) => header.number === 2 && header.wire === 2,
    )?.bytes;
    if (internalExt) break;
  }
  if (!internalExt?.length) return null;

  return Buffer.concat([
    protoVarintField(2, sequence),
    protoBytesField(7, "ack"),
    protoBytesField(8, internalExt),
  ]);
}

function buildDouyinHeartbeat() {
  return protoBytesField(7, "hb");
}

function createDirectWebSocket({
  url,
  headers,
  initialFrames,
  onBinary,
  onClose,
}) {
  const target = new URL(url);
  const port = Number(target.port || 443);
  const websocketKey = randomBytes(16).toString("base64");
  const expectedAccept = createHash("sha1")
    .update(websocketKey + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")
    .digest("base64");

  let socket = null;
  let buffer = Buffer.alloc(0);
  let upgraded = false;
  let closed = false;
  let closeNotified = false;
  let fragmentOpcode = 0;
  let fragments = [];
  let readyResolve;
  let readyReject;
  let firstBinaryResolve;
  let firstBinarySeen = false;

  const ready = new Promise((resolve, reject) => {
    readyResolve = resolve;
    readyReject = reject;
  });
  const firstBinary = new Promise((resolve) => {
    firstBinaryResolve = resolve;
  });

  const notifyClosed = (reason) => {
    if (closeNotified) return;
    closeNotified = true;
    const error =
      reason instanceof Error ? reason : new Error(String(reason || "closed"));
    if (!upgraded) readyReject(error);
    if (!firstBinarySeen) firstBinaryResolve(false);
    onClose?.(error);
  };

  const writeFrame = (opcode, payload) => {
    if (!socket || socket.destroyed || !upgraded) return;
    socket.write(encodeClientWebSocketFrame(opcode, payload));
  };

  const deliverMessage = (opcode, payload) => {
    if (opcode !== 2) return;
    if (!firstBinarySeen) {
      firstBinarySeen = true;
      firstBinaryResolve(true);
    }
    onBinary?.(payload);
  };

  const parseFrames = () => {
    while (buffer.length >= 2) {
      const first = buffer[0];
      const second = buffer[1];
      const fin = (first & 0x80) !== 0;
      const opcode = first & 0x0f;
      const masked = (second & 0x80) !== 0;
      let length = second & 0x7f;
      let offset = 2;

      if (length === 126) {
        if (buffer.length < 4) return;
        length = buffer.readUInt16BE(2);
        offset = 4;
      } else if (length === 127) {
        if (buffer.length < 10) return;
        const length64 = buffer.readBigUInt64BE(2);
        if (length64 > BigInt(Number.MAX_SAFE_INTEGER)) {
          notifyClosed(new Error("websocket frame is too large"));
          socket?.destroy();
          return;
        }
        length = Number(length64);
        offset = 10;
      }

      let mask;
      if (masked) {
        if (buffer.length < offset + 4) return;
        mask = buffer.subarray(offset, offset + 4);
        offset += 4;
      }
      if (buffer.length < offset + length) return;

      let payload = buffer.subarray(offset, offset + length);
      buffer = buffer.subarray(offset + length);

      if (masked && mask) {
        const unmasked = Buffer.alloc(payload.length);
        for (let index = 0; index < payload.length; index += 1) {
          unmasked[index] = payload[index] ^ mask[index % 4];
        }
        payload = unmasked;
      }

      if (opcode === 0x8) {
        if (!closed) {
          closed = true;
          try {
            writeFrame(0x8, payload);
          } catch {}
        }
        socket?.end();
        notifyClosed(new Error("websocket closed by server"));
        return;
      }
      if (opcode === 0x9) {
        writeFrame(0x0a, payload);
        continue;
      }
      if (opcode === 0x0a) continue;

      if (opcode === 0x0) {
        if (!fragmentOpcode) continue;
        fragments.push(payload);
        if (fin) {
          deliverMessage(fragmentOpcode, Buffer.concat(fragments));
          fragmentOpcode = 0;
          fragments = [];
        }
        continue;
      }

      if (opcode !== 0x1 && opcode !== 0x2) continue;
      if (fin) {
        deliverMessage(opcode, payload);
      } else {
        fragmentOpcode = opcode;
        fragments = [payload];
      }
    }
  };

  socket = tls.connect({
    host: target.hostname,
    port,
    servername: target.hostname,
    rejectUnauthorized: true,
  });

  socket.setTimeout(15000, () => {
    notifyClosed(new Error("websocket handshake timeout"));
    socket.destroy();
  });

  socket.on("secureConnect", () => {
    const requestHeaders = [
      "GET " + target.pathname + target.search + " HTTP/1.1",
      "Host: " + target.host,
      "Upgrade: websocket",
      "Connection: Upgrade",
      "Sec-WebSocket-Key: " + websocketKey,
      "Sec-WebSocket-Version: 13",
    ];

    const skipHeaders = new Set([
      "host",
      "connection",
      "upgrade",
      "sec-websocket-key",
      "sec-websocket-version",
      "sec-websocket-extensions",
      "content-length",
    ]);
    for (const [key, value] of Object.entries(headers ?? {})) {
      const lowerKey = String(key).toLowerCase();
      if (skipHeaders.has(lowerKey) || value == null || value === "") continue;
      requestHeaders.push(String(key) + ": " + String(value));
    }
    requestHeaders.push("", "");
    socket.write(requestHeaders.join("\r\n"));
  });

  socket.on("data", (chunk) => {
    buffer = Buffer.concat([buffer, chunk]);

    if (!upgraded) {
      const headerEnd = buffer.indexOf("\r\n\r\n");
      if (headerEnd < 0) return;

      const responseHead = buffer.subarray(0, headerEnd).toString("utf8");
      buffer = buffer.subarray(headerEnd + 4);
      const lines = responseHead.split("\r\n");
      const statusLine = lines.shift() ?? "";
      if (!/\s101\s/.test(statusLine)) {
        notifyClosed(new Error("websocket upgrade failed: " + statusLine));
        socket.destroy();
        return;
      }

      const responseHeaders = {};
      for (const line of lines) {
        const colon = line.indexOf(":");
        if (colon <= 0) continue;
        responseHeaders[line.slice(0, colon).trim().toLowerCase()] = line
          .slice(colon + 1)
          .trim();
      }
      if (responseHeaders["sec-websocket-accept"] !== expectedAccept) {
        notifyClosed(new Error("invalid websocket accept header"));
        socket.destroy();
        return;
      }

      upgraded = true;
      socket.setTimeout(0);
      for (const frame of initialFrames ?? []) {
        const decoded = cdpFramePayload(frame);
        if (decoded) writeFrame(decoded.opcode, decoded.payload);
      }
      readyResolve();
    }

    parseFrames();
  });

  socket.on("error", (error) => {
    notifyClosed(error);
  });
  socket.on("close", () => {
    notifyClosed(new Error("websocket socket closed"));
  });

  return {
    ready,
    firstBinary,
    get closed() {
      return closed || socket?.destroyed === true;
    },
    sendBinary(payload) {
      writeFrame(0x2, payload);
    },
    close() {
      if (closed) return;
      closed = true;
      try {
        writeFrame(0x8, Buffer.alloc(0));
      } catch {}
      try {
        socket?.end();
      } catch {}
      setTimeout(() => {
        try {
          socket?.destroy();
        } catch {}
      }, 250).unref?.();
    },
  };
}

async function ensureBrowser() {
  if (browser?.isConnected() && browserContext) return;

  if (browserPromise) {
    await browserPromise;
    return;
  }

  browserPromise = (async () => {
    const executablePath = detectChrome();
    if (!executablePath) {
      throw new Error("Chrome/Chromium executable not found");
    }

    log("launching browser: " + executablePath);

    browser = await chromium.launch({
      executablePath,
      headless: isHeadless(),
      args: [
        "--disable-blink-features=AutomationControlled",
        "--autoplay-policy=no-user-gesture-required",
        "--no-sandbox",
        "--disable-dev-shm-usage",
        "--mute-audio",
      ],
    });

    browser.on("disconnected", () => {
      browser = null;
      browserContext = null;
      browserPromise = null;
    });

    browserContext = await browser.newContext({
      userAgent: DEFAULT_USER_AGENT,
      viewport: { width: 1440, height: 900 },
      locale: "zh-CN",
      serviceWorkers: "block",
    });

    await browserContext.addInitScript(() => {
      Object.defineProperty(navigator, "webdriver", {
        get: () => undefined,
      });

      // The always-on collector only needs DOM/live-state plus
      // network/WebSocket data. Media discovery is a separate on-demand
      // operation used only when Core starts an audio recording.
      const stopMediaElement = (element) => {
        if (!(element instanceof HTMLMediaElement)) return;
        try {
          element.autoplay = false;
          element.muted = true;
          element.pause();
        } catch {}
      };

      HTMLMediaElement.prototype.play = function () {
        stopMediaElement(this);
        return Promise.resolve();
      };

      const suppressPageMedia = () => {
        document.querySelectorAll("video,audio").forEach(stopMediaElement);
      };

      const installMediaObserver = () => {
        suppressPageMedia();
        const root = document.documentElement ?? document;
        new MutationObserver(suppressPageMedia).observe(root, {
          childList: true,
          subtree: true,
        });
        document.addEventListener(
          "play",
          (event) => stopMediaElement(event.target),
          true,
        );
      };

      if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", installMediaObserver, {
          once: true,
        });
      } else {
        installMediaObserver();
      }
    });

    log("browser ready");
  })();

  try {
    await browserPromise;
  } finally {
    browserPromise = null;
  }
}

async function stopRoom(roomId, notify = true) {
  const key = String(roomId);
  const session = rooms.get(key);
  if (!session) {
    if (notify) send({ type: "room_stopped", room_id: roomId });
    return;
  }

  rooms.delete(key);
  session.stopped = true;
  session.intentionalPageClose = true;

  if (session.liveEndedTimer) {
    clearInterval(session.liveEndedTimer);
    session.liveEndedTimer = null;
  }
  if (session.directHeartbeatTimer) {
    clearInterval(session.directHeartbeatTimer);
    session.directHeartbeatTimer = null;
  }

  try {
    session.directSocket?.close();
  } catch {}
  session.directSocket = null;
  session.directConnected = false;

  try {
    await session.cdp?.detach();
  } catch {}

  try {
    await session.page?.close();
  } catch {}

  if (notify) send({ type: "room_stopped", room_id: roomId });
}

function classifyStreamCandidate(url, contentType = "", resourceType = "") {
  const lowerURL = String(url ?? "").toLowerCase();
  const lowerType = String(contentType ?? "").toLowerCase();

  if (
    lowerURL.includes(".m3u8") ||
    lowerType.includes("application/vnd.apple.mpegurl") ||
    lowerType.includes("application/x-mpegurl")
  ) {
    return "hls";
  }

  if (
    lowerURL.includes(".flv") ||
    lowerType.includes("video/x-flv") ||
    lowerType.includes("application/x-flv")
  ) {
    return "flv";
  }

  if (
    resourceType === "media" &&
    (lowerURL.includes("pull") ||
      lowerURL.includes("stream") ||
      lowerURL.includes("live"))
  ) {
    return "media";
  }

  return "";
}

async function configureTextOnlyCollectorPage(page) {
  await page.route("**/*", async (route) => {
    const request = route.request();
    const resourceType = request.resourceType();
    const protocol = classifyStreamCandidate(request.url(), "", resourceType);
    if (
      resourceType === "image" ||
      resourceType === "font" ||
      resourceType === "media" ||
      protocol
    ) {
      await route.abort();
      return;
    }
    await route.continue();
  });
}

function emitRoomFrame(session, roomId, payload) {
  if (session.stopped || !payload) return;
  const payloadData = Buffer.isBuffer(payload)
    ? payload.toString("base64")
    : String(payload);
  if (!payloadData) return;

  session.frameCount += 1;
  send({
    type: "frame",
    room_id: roomId,
    payload: payloadData,
  });
  if (session.frameCount === 1) {
    send({
      type: "transport_live",
      room_id: roomId,
    });
  }
}

function preferredBootstrapSocket(session) {
  const candidates = [...session.webSocketCandidates.values()].filter(
    (candidate) =>
      /^wss:\/\//i.test(candidate.url ?? "") &&
      candidate.sentFrames.length > 0,
  );
  candidates.sort((left, right) => {
    const leftPreferred = left.url.includes("/webcast/im/push/v2/") ? 1 : 0;
    const rightPreferred = right.url.includes("/webcast/im/push/v2/") ? 1 : 0;
    return rightPreferred - leftPreferred;
  });
  return candidates[0] ?? null;
}

async function waitForDirectBootstrap(session, timeoutMS) {
  const deadline = Date.now() + timeoutMS;
  while (!session.stopped && Date.now() < deadline) {
    const candidate = preferredBootstrapSocket(session);
    if (candidate) return candidate;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  return null;
}

async function closeBootstrapPage(session) {
  session.intentionalPageClose = true;
  if (session.liveEndedTimer) {
    clearInterval(session.liveEndedTimer);
    session.liveEndedTimer = null;
  }
  try {
    await session.cdp?.detach();
  } catch {}
  try {
    await session.page?.close();
  } catch {}
  session.cdp = null;
  session.page = null;
}

async function activateDirectTransport(session, roomId) {
  const candidate = await waitForDirectBootstrap(
    session,
    DIRECT_WS_BOOTSTRAP_TIMEOUT_MS,
  );
  if (!candidate || session.stopped) return false;

  session.selectedBrowserSocketRequestID = candidate.requestId;
  const cookies = await browserContext.cookies().catch(() => []);
  const headers = { ...(candidate.headers ?? {}) };
  if (!headerValue(headers, "cookie")) {
    const cookie = cookieHeaderForURL(cookies, candidate.url);
    if (cookie) headers.Cookie = cookie;
  }
  if (!headerValue(headers, "origin")) {
    headers.Origin = "https://live.douyin.com";
  }
  if (!headerValue(headers, "user-agent")) {
    headers["User-Agent"] = DEFAULT_USER_AGENT;
  }
  if (!headerValue(headers, "accept-language")) {
    headers["Accept-Language"] = "zh-CN,zh;q=0.9";
  }

  let direct;
  try {
    direct = createDirectWebSocket({
      url: candidate.url,
      headers,
      initialFrames: candidate.sentFrames,
      onBinary: (payload) => {
        if (session.stopped) return;
        const ack = buildDouyinAck(payload);
        if (ack) {
          try {
            direct?.sendBinary(ack);
          } catch {}
        }
        session.directConnected = true;
        emitRoomFrame(session, roomId, payload);
      },
      onClose: (error) => {
        if (session.directHeartbeatTimer) {
          clearInterval(session.directHeartbeatTimer);
          session.directHeartbeatTimer = null;
        }
        if (session.stopped || !session.directConnected) return;
        session.directConnected = false;
        send({
          type: "room_error",
          room_id: roomId,
          error:
            "douyin direct websocket closed: " +
            (error instanceof Error ? error.message : String(error)),
        });
      },
    });
    session.directSocket = direct;
    await direct.ready;
    direct.sendBinary(buildDouyinHeartbeat());
    const receivedFirstBinary = await Promise.race([
      direct.firstBinary,
      new Promise((resolve) => {
        setTimeout(() => resolve(false), DIRECT_WS_BOOTSTRAP_TIMEOUT_MS);
      }),
    ]);
    if (!receivedFirstBinary || !session.directConnected || direct.closed) {
      throw new Error("direct websocket closed before activation");
    }
    session.directHeartbeatTimer = setInterval(() => {
      if (session.stopped || !session.directConnected || direct.closed) return;
      try {
        direct.sendBinary(buildDouyinHeartbeat());
      } catch {}
    }, DIRECT_WS_HEARTBEAT_MS);

    session.browserFrameBuffer = [];
    log(
      "room " +
        roomId +
        " direct websocket active host=" +
        new URL(candidate.url).host,
    );
    await closeBootstrapPage(session);
    return true;
  } catch (error) {
    try {
      direct?.close();
    } catch {}
    if (session.directSocket === direct) session.directSocket = null;
    session.directConnected = false;
    if (session.directHeartbeatTimer) {
      clearInterval(session.directHeartbeatTimer);
      session.directHeartbeatTimer = null;
    }
    log(
      "room " +
        roomId +
        " direct websocket fallback: " +
        (error instanceof Error ? error.message : String(error)),
    );
    return false;
  }
}

async function startRoom(command) {
  const roomId = command.room_id;
  const url = String(command.url ?? "").trim();

  if (!roomId || !url) {
    throw new Error("room_id and url are required");
  }

  await stopRoom(roomId, false);
  await ensureBrowser();

  const page = await browserContext.newPage();
  await configureTextOnlyCollectorPage(page);

  const cdp = await browserContext.newCDPSession(page);

  const session = {
    url,
    page,
    cdp,
    stopped: false,
    frameCount: 0,
    streamCandidates: new Set(),
    liveEndedTimer: null,
    liveEndedReported: false,
    webSocketCandidates: new Map(),
    selectedBrowserSocketRequestID: "",
    browserFrameBuffer: [],
    forwardBrowserFrames: false,
    directSocket: null,
    directConnected: false,
    directHeartbeatTimer: null,
    intentionalPageClose: false,
  };
  rooms.set(String(roomId), session);

  try {
    await cdp.send("Network.enable");

    cdp.on("Network.webSocketCreated", (params) => {
      if (session.stopped || !params?.requestId || !params?.url) return;
      session.webSocketCandidates.set(params.requestId, {
        requestId: params.requestId,
        url: params.url,
        headers: {},
        sentFrames: [],
      });
    });

    cdp.on("Network.webSocketWillSendHandshakeRequest", (params) => {
      if (session.stopped || !params?.requestId) return;
      const candidate = session.webSocketCandidates.get(params.requestId);
      if (!candidate) return;
      candidate.headers = { ...(params.request?.headers ?? {}) };
    });

    cdp.on("Network.webSocketFrameSent", (params) => {
      if (session.stopped || !params?.requestId) return;
      const candidate = session.webSocketCandidates.get(params.requestId);
      if (
        !candidate ||
        candidate.sentFrames.length >= MAX_BOOTSTRAP_CLIENT_FRAMES ||
        !params.response?.payloadData
      ) {
        return;
      }
      candidate.sentFrames.push({
        opcode: params.response.opcode,
        payloadData: params.response.payloadData,
      });
    });

    cdp.on("Network.webSocketFrameReceived", (params) => {
      if (session.stopped) return;

      const response = params?.response;
      const payloadData = response?.payloadData;
      if (!payloadData || response?.opcode === 1) return;
      if (session.forwardBrowserFrames) {
        emitRoomFrame(session, roomId, payloadData);
        return;
      }
      if (session.browserFrameBuffer.length >= MAX_BOOTSTRAP_BROWSER_FRAMES) {
        session.browserFrameBuffer.shift();
      }
      session.browserFrameBuffer.push(payloadData);
    });

    cdp.on("Network.webSocketClosed", (params) => {
      if (
        session.stopped ||
        session.intentionalPageClose ||
        session.directConnected
      ) {
        return;
      }
      if (
        session.selectedBrowserSocketRequestID &&
        params?.requestId !== session.selectedBrowserSocketRequestID
      ) {
        return;
      }
      send({
        type: "room_error",
        room_id: roomId,
        error: "douyin browser websocket closed",
      });
    });

    page.on("close", () => {
      if (
        !session.stopped &&
        !session.intentionalPageClose &&
        !session.directConnected
      ) {
        send({
          type: "room_error",
          room_id: roomId,
          error: "douyin page closed",
        });
      }
    });

    log("room " + roomId + " navigating to " + url);

    await page.goto(url, {
      waitUntil: "domcontentloaded",
      timeout: 45000,
    });

    try {
      await page.waitForFunction(
        () => document.title && document.title.length > 0,
        { timeout: 15000 },
      );
    } catch {}

    await page.waitForTimeout(2500);

    const reportLiveEndedFromDOM = async () => {
      if (session.stopped || session.liveEndedReported) return;
      const ended = await page
        .evaluate(() => Boolean(document.body?.innerText?.includes("直播已结束")))
        .catch(() => false);
      if (!ended) return;

      session.liveEndedReported = true;
      if (session.liveEndedTimer) {
        clearInterval(session.liveEndedTimer);
        session.liveEndedTimer = null;
      }
      send({
        type: "room_state",
        room_id: roomId,
        state: "ended",
        reason: "page_live_ended_text",
      });
    };

    await reportLiveEndedFromDOM();
    if (session.liveEndedReported) {
      return;
    }

    try {
      await page.mouse.click(720, 450);
    } catch {}

    const finalURL = page.url();
    const title = await page.title().catch(() => "");
    let directActive = false;
    if (isDirectWebSocketEnabled()) {
      directActive = await activateDirectTransport(session, roomId);
    }

    if (!directActive) {
      session.forwardBrowserFrames = true;
      for (const payload of session.browserFrameBuffer) {
        emitRoomFrame(session, roomId, payload);
      }
      session.browserFrameBuffer = [];
      if (!session.liveEndedReported) {
        session.liveEndedTimer = setInterval(() => {
          void reportLiveEndedFromDOM();
        }, 2000);
      }
      log("room " + roomId + " using browser websocket fallback");
    }

    send({
      type: "room_started",
      room_id: roomId,
      final_url: finalURL,
      title,
    });
  } catch (error) {
    await stopRoom(roomId, false);
    throw error;
  }
}

async function resolveStream(command) {
  const roomId = command.room_id;
  const requestId = String(command.request_id ?? "").trim();
  const url = String(command.url ?? "").trim();

  if (!roomId || !requestId || !url) {
    throw new Error("room_id, request_id and url are required");
  }

  const roomSession = rooms.get(String(roomId));
  if (!roomSession || roomSession.stopped) {
    throw new Error("room session is not active");
  }

  await ensureBrowser();

  const probeContext = await browser.newContext({
    userAgent: DEFAULT_USER_AGENT,
    viewport: { width: 1440, height: 900 },
    locale: "zh-CN",
    serviceWorkers: "block",
  });
  await probeContext.addInitScript(() => {
    Object.defineProperty(navigator, "webdriver", {
      get: () => undefined,
    });
  });

  const page = await probeContext.newPage();
  let settled = false;
  let resolveCandidate;
  const candidatePromise = new Promise((resolve) => {
    resolveCandidate = resolve;
  });

  const settleCandidate = (candidate) => {
    if (settled) return;
    settled = true;
    resolveCandidate(candidate);
  };

  await page.route("**/*", async (route) => {
    const request = route.request();
    const resourceType = request.resourceType();
    const requestURL = request.url();
    const protocol = classifyStreamCandidate(
      requestURL,
      "",
      resourceType,
    );

    if (protocol) {
      settleCandidate({
        protocol,
        resourceType,
        url: requestURL,
      });
      await route.abort();
      return;
    }

    if (resourceType === "image" || resourceType === "font") {
      await route.abort();
      return;
    }

    await route.continue();
  });

  try {
    const navigation = page
      .goto(url, {
        waitUntil: "domcontentloaded",
        timeout: 12000,
      })
      .catch(() => null);

    void navigation.then(async () => {
      try {
        await page.mouse.click(720, 450);
      } catch {}
    });

    const candidate = await Promise.race([
      candidatePromise,
      new Promise((resolve) => {
        setTimeout(() => resolve(null), 6000);
      }),
    ]);

    if (!candidate) {
      throw new Error("live stream source was not discovered");
    }

    log(
      "room " +
        roomId +
        " on-demand stream protocol=" +
        candidate.protocol +
        " type=" +
        candidate.resourceType,
    );

    send({
      type: "stream_candidate",
      room_id: roomId,
      request_id: requestId,
      protocol: candidate.protocol,
      resource_type: candidate.resourceType,
      url: candidate.url,
    });
  } finally {
    try {
      await probeContext.close();
    } catch {}
  }
}

async function capturePreview(command) {
  const roomId = command.room_id;
  const requestId = String(command.request_id ?? "").trim();
  const session = rooms.get(String(roomId));

  if (!roomId || !requestId) {
    throw new Error("room_id and request_id are required");
  }
  if (!session || session.stopped) {
    throw new Error("room session is not active");
  }

  let page = session.page;
  let temporaryPage = false;
  if (!page) {
    await ensureBrowser();
    page = await browserContext.newPage();
    temporaryPage = true;
    await configureTextOnlyCollectorPage(page);
    await page.goto(session.url, {
      waitUntil: "domcontentloaded",
      timeout: 12000,
    });
    await page.waitForTimeout(1200);
  }

  try {
    let clip;
    try {
      clip = await page.evaluate(() => {
        const candidates = [...document.querySelectorAll("video")]
          .map((element) => {
            const rect = element.getBoundingClientRect();
            return {
              x: Math.max(0, rect.x),
              y: Math.max(0, rect.y),
              width: Math.max(0, rect.width),
              height: Math.max(0, rect.height),
              area: Math.max(0, rect.width) * Math.max(0, rect.height),
            };
          })
          .filter(
            (item) =>
              item.area > 20000 &&
              item.width > 200 &&
              item.height > 120,
          )
          .sort((a, b) => b.area - a.area);

        return candidates[0] ?? null;
      });
    } catch {}

    if (clip) {
      clip = {
        x: Math.floor(clip.x),
        y: Math.floor(clip.y),
        width: Math.max(
          1,
          Math.min(1440 - Math.floor(clip.x), Math.floor(clip.width)),
        ),
        height: Math.max(
          1,
          Math.min(900 - Math.floor(clip.y), Math.floor(clip.height)),
        ),
      };
    }

    const image = await page.screenshot({
      type: "jpeg",
      quality: 55,
      fullPage: false,
      ...(clip ? { clip } : {}),
    });

    send({
      type: "preview_frame",
      room_id: roomId,
      request_id: requestId,
      content_type: "image/jpeg",
      payload: image.toString("base64"),
    });
  } finally {
    if (temporaryPage) {
      try {
        await page.close();
      } catch {}
    }
  }
}
async function handleCommand(command) {
  switch (command?.op) {
    case "start":
      try {
        await startRoom(command);
      } catch (error) {
        send({
          type: "room_error",
          room_id: command.room_id,
          error: error instanceof Error ? error.message : String(error),
        });
      }
      return;

    case "stop":
      await stopRoom(command.room_id);
      return;

    case "ping":
      send({ type: "pong" });
      return;

    case "preview":
      try {
        await capturePreview(command);
      } catch (error) {
        send({
          type: "preview_error",
          room_id: command.room_id,
          request_id: command.request_id,
          error: error instanceof Error ? error.message : String(error),
        });
      }
      return;

    case "resolve_stream":
      try {
        await resolveStream(command);
      } catch (error) {
        send({
          type: "stream_error",
          room_id: command.room_id,
          request_id: command.request_id,
          error: error instanceof Error ? error.message : String(error),
        });
      }
      return;

    case "shutdown":
      await shutdown();
      process.exit(0);
      return;

    default:
      send({
        type: "worker_error",
        error: "unsupported command",
      });
  }
}

async function shutdown() {
  const ids = [...rooms.keys()];
  for (const id of ids) {
    await stopRoom(id, false);
  }

  try {
    await browserContext?.close();
  } catch {}

  try {
    await browser?.close();
  } catch {}

  browserContext = null;
  browser = null;
}

const readline = createInterface({
  input: process.stdin,
  crlfDelay: Infinity,
});

readline.on("line", (line) => {
  const value = line.trim();
  if (!value) return;

  let command;
  try {
    command = JSON.parse(value);
  } catch (error) {
    send({
      type: "worker_error",
      error: "invalid command json",
    });
    return;
  }

  handleCommand(command).catch((error) => {
    send({
      type: "worker_error",
      error: error instanceof Error ? error.message : String(error),
    });
  });
});

process.on("SIGINT", async () => {
  await shutdown();
  process.exit(0);
});

process.on("SIGTERM", async () => {
  await shutdown();
  process.exit(0);
});

send({
  type: "ready",
  pid: process.pid,
});
