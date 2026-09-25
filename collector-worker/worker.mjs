import { createInterface } from "node:readline";
import { existsSync } from "node:fs";
import { chromium } from "playwright-core";

const DEFAULT_USER_AGENT =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36";

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
    });

    await browserContext.addInitScript(() => {
      Object.defineProperty(navigator, "webdriver", {
        get: () => undefined,
      });
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
  if (session.stateCheckTimer) clearTimeout(session.stateCheckTimer);
  if (session.statePollTimer) clearInterval(session.statePollTimer);

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

const OFFLINE_ROOM_TEXTS = [
  "\u76f4\u64ad\u5df2\u7ed3\u675f",
  "\u5f53\u524d\u76f4\u64ad\u5df2\u7ed3\u675f",
  "\u4e3b\u64ad\u5df2\u4e0b\u64ad",
  "\u5df2\u4e0b\u64ad",
  "\u6682\u672a\u5f00\u64ad",
  "\u4e3b\u64ad\u6682\u672a\u5f00\u64ad",
  "\u76f4\u64ad\u6682\u672a\u5f00\u59cb",
];

const LIVE_EVIDENCE_TTL_MS = 15000;

function markLiveEvidence(session, reason) {
  if (!session || session.stopped || session.offlineLatched) return;
  session.lastLiveEvidenceAt = Date.now();
  session.lastLiveEvidenceReason = reason;
}

function hasRecentLiveEvidence(session) {
  return (
    session.lastLiveEvidenceAt > 0 &&
    Date.now() - session.lastLiveEvidenceAt <= LIVE_EVIDENCE_TTL_MS
  );
}

function publishRoomState(session, roomId, state, reason) {
  if (!session || session.stopped) return;
  if (state === "live" && session.offlineLatched) return;
  if (state === "offline") {
    session.offlineLatched = true;
    session.streamCandidates.clear();
    session.lastLiveEvidenceAt = 0;
    session.lastLiveEvidenceReason = "";
  }
  if (session.roomState === state) return;
  session.roomState = state;
  session.roomStateReason = reason;
  send({
    type: "room_state",
    room_id: roomId,
    state,
    reason,
  });
  log("room " + roomId + " state=" + state + " reason=" + reason);
}

async function inspectRoomPageState(session, roomId, finalizeUnknown = false) {
  if (!session || session.stopped || session.stateProbeBusy) return;
  session.stateProbeBusy = true;
  try {
    let snapshot = null;
    try {
      snapshot = await session.page.evaluate((offlineTexts) => {
        const bodyText = String(document.body?.innerText ?? "").replace(/\s+/g, " ");
        const offlineText = offlineTexts.find((text) => bodyText.includes(text)) ?? "";
        const liveVideo = [...document.querySelectorAll("video")].find((video) => {
          const rect = video.getBoundingClientRect();
          const visible = rect.width > 200 && rect.height > 120;
          const durationLooksLive =
            !Number.isFinite(video.duration) ||
            video.duration === 0 ||
            String(video.currentSrc || video.src || "").startsWith("blob:");
          return (
            visible &&
            !video.ended &&
            video.readyState >= 2 &&
            video.currentTime > 0 &&
            durationLooksLive
          );
        });
        return {
          offlineText,
          liveVideo: Boolean(liveVideo),
          liveVideoTime: Number(liveVideo?.currentTime ?? 0),
        };
      }, OFFLINE_ROOM_TEXTS);
    } catch {}

    if (snapshot?.liveVideo) {
      const videoTime = Number(snapshot.liveVideoTime) || 0;
      const videoRestarted = videoTime + 1 < session.lastVideoCurrentTime;
      const videoProgressed =
        session.lastVideoCurrentTime <= 0 ||
        videoRestarted ||
        videoTime > session.lastVideoCurrentTime + 0.2;

      if (videoProgressed) {
        session.lastVideoCurrentTime = videoTime;
        session.lastVideoProgressAt = Date.now();
        markLiveEvidence(session, "video_playing");
      }
    }

    if (hasRecentLiveEvidence(session)) {
      publishRoomState(
        session,
        roomId,
        "live",
        session.lastLiveEvidenceReason || "live_evidence",
      );
      return;
    }

    // Douyin can leave hidden/stale "live ended" text in the DOM while the
    // room is still producing real media or push frames. Live transport/video
    // evidence is authoritative; page text is only allowed to declare offline
    // after the recent-evidence window has expired.
    if (snapshot?.offlineText) {
      publishRoomState(session, roomId, "offline", "page_live_ended");
      return;
    }

    if (finalizeUnknown) {
      publishRoomState(session, roomId, "offline", "no_live_media");
    }
  } finally {
    session.stateProbeBusy = false;
  }
}

function scheduleRoomStateChecks(session, roomId) {
  if (!session || session.stopped) return;
  void inspectRoomPageState(session, roomId, false);
  session.stateCheckTimer = setTimeout(() => {
    void inspectRoomPageState(session, roomId, true);
  }, 12000);
  session.statePollTimer = setInterval(() => {
    void inspectRoomPageState(session, roomId, true);
  }, 5000);
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
  const cdp = await browserContext.newCDPSession(page);

  const session = {
    page,
    cdp,
    stopped: false,
    frameCount: 0,
    streamCandidates: new Set(),
    roomState: "",
    roomStateReason: "",
    offlineLatched: false,
    lastLiveEvidenceAt: 0,
    lastLiveEvidenceReason: "",
    lastVideoCurrentTime: 0,
    lastVideoProgressAt: 0,
    stateCheckTimer: null,
    statePollTimer: null,
    stateProbeBusy: false,
  };
  rooms.set(String(roomId), session);

  try {
    await cdp.send("Network.enable");

    page.on("response", async (response) => {
      if (session.stopped || session.offlineLatched) return;

      const request = response.request();
      const responseURL = response.url();

      let headers = {};
      try {
        headers = await response.allHeaders();
      } catch {}

      const contentType =
        headers["content-type"] ??
        headers["Content-Type"] ??
        "";

      const protocol = classifyStreamCandidate(
        responseURL,
        contentType,
        request.resourceType(),
      );
      if (!protocol) return;

      markLiveEvidence(session, "stream_" + protocol);
      publishRoomState(session, roomId, "live", "stream_" + protocol);

      const key = protocol + "|" + responseURL;
      if (session.streamCandidates.has(key)) return;
      if (session.streamCandidates.size >= 12) {
        session.streamCandidates.clear();
      }
      session.streamCandidates.add(key);

      log(
        "room " +
          roomId +
          " stream candidate protocol=" +
          protocol +
          " type=" +
          request.resourceType() +
          " url=" +
          responseURL,
      );

      send({
        type: "stream_candidate",
        room_id: roomId,
        protocol,
        resource_type: request.resourceType(),
        content_type: contentType,
        url: responseURL,
      });
    });
    cdp.on("Network.webSocketFrameReceived", (params) => {
      if (session.stopped) return;

      const response = params?.response;
      const payloadData = response?.payloadData;
      if (!payloadData || response?.opcode === 1) return;

      session.frameCount += 1;
      if (session.roomState === "live") {
        markLiveEvidence(session, "push_frame");
      }
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
    });

    cdp.on("Network.webSocketClosed", () => {
      if (session.stopped || session.offlineLatched) return;

      // A transport socket can reconnect while the anchor is still live.
      // Drop transport-only freshness and let current page/video/media evidence
      // decide the actual broadcast state instead of equating disconnect=offline.
      session.lastLiveEvidenceAt = 0;
      session.lastLiveEvidenceReason = "";
      void inspectRoomPageState(session, roomId, true);
    });

    page.on("close", () => {
      if (!session.stopped) {
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

    try {
      await page.mouse.click(720, 450);
    } catch {}

    send({
      type: "room_started",
      room_id: roomId,
      final_url: page.url(),
      title: await page.title().catch(() => ""),
    });

    scheduleRoomStateChecks(session, roomId);
  } catch (error) {
    await stopRoom(roomId, false);
    throw error;
  }
}

async function capturePreview(command) {
  const roomId = command.room_id;
  const requestId = String(command.request_id ?? "").trim();
  const session = rooms.get(String(roomId));

  if (!roomId || !requestId) {
    throw new Error("room_id and request_id are required");
  }
  if (!session || session.stopped || !session.page) {
    throw new Error("room session is not active");
  }

  const page = session.page;

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
      width: Math.max(1, Math.min(1440 - Math.floor(clip.x), Math.floor(clip.width))),
      height: Math.max(1, Math.min(900 - Math.floor(clip.y), Math.floor(clip.height))),
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