const nowPlaying = document.getElementById("now-playing");
const player = document.getElementById("player");
const overlay = document.getElementById("overlay");
const overlayText = document.getElementById("overlay-text");
const stage = document.getElementById("stage");
const fsTitle = document.getElementById("fs-title");
const fsDetail = document.getElementById("fs-detail");
const fsToggle = document.getElementById("fs-toggle");

let videos = [];
let current = null;
let hls = null;
let selectedTrack = 0;
let playbackStatusText = "";
let acceptEnd = false;
let upcoming = [];

function setStatus(text) {
  playbackStatusText = text;
}

function setOverlay(show, text) {
  overlay.classList.toggle("hidden", !show);
  if (text) overlayText.textContent = text;
}

function destroyHls() {
  acceptEnd = false;
  if (hls) {
    hls.destroy();
    hls = null;
  }
  player.removeAttribute("src");
}

function trackLabel(index) {
  const t = current?.audioTracks?.[index];
  return t?.title || `track ${index + 1}`;
}

function trackNames(video) {
  return (video?.audioTracks || []).map((t, i) => t.title || `Track ${i + 1}`);
}

function reportState(extra = {}) {
  const body = {
    file: current?.path || "",
    name: current?.name || "",
    track: selectedTrack,
    paused: player.paused,
    playing: !player.paused && player.readyState > 2,
    status: playbackStatusText,
    tracks: trackNames(current),
    ...extra,
  };
  fetch("/api/state", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  }).catch(() => {});
}

function selectTrack(index) {
  selectedTrack = index;
  if (hls && hls.audioTracks && hls.audioTracks.length) {
    hls.audioTrack = index;
    return;
  }
  const native = player.audioTracks;
  if (native && native.length) {
    for (let i = 0; i < native.length; i++) {
      native[i].enabled = i === index;
    }
  }
}

async function tryAutoplay() {
  try {
    await player.play();
    return { playing: true, muted: player.muted };
  } catch {
    const wasMuted = player.muted;
    player.muted = true;
    try {
      await player.play();
      return { playing: true, muted: true, mutedFallback: !wasMuted };
    } catch {
      player.muted = wasMuted;
      return { playing: false, muted: player.muted };
    }
  }
}

function fullscreenElement() {
  return document.fullscreenElement || document.webkitFullscreenElement || null;
}

function nextSongLine() {
  const next = upcoming[0];
  if (next) return `Next song: ${next.name || next.file}`;
  return "No next song. Scan the QR code to choose one.";
}

function updateFullscreenCaption() {
  fsTitle.textContent = current?.name || nowPlaying.textContent;
  fsDetail.textContent = current ? nextSongLine() : "";
  fsToggle.textContent = fullscreenElement() === stage ? "Exit fullscreen" : "Fullscreen";
}

let movingFullscreen = false;

async function enterStageFullscreen() {
  if (stage.requestFullscreen) {
    await stage.requestFullscreen();
    return;
  }
  if (stage.webkitRequestFullscreen) {
    stage.webkitRequestFullscreen();
  }
}

async function onFullscreenChange() {
  if (!movingFullscreen && fullscreenElement() === player) {
    movingFullscreen = true;
    try {
      if (document.exitFullscreen) await document.exitFullscreen();
      else if (document.webkitExitFullscreen) document.webkitExitFullscreen();
      await enterStageFullscreen();
    } catch {
      // The browser kept fullscreen on the video element, which cannot show HTML text.
    } finally {
      movingFullscreen = false;
    }
  }
  updateFullscreenCaption();
}

document.addEventListener("fullscreenchange", onFullscreenChange);
document.addEventListener("webkitfullscreenchange", onFullscreenChange);
fsToggle.addEventListener("click", async () => {
  try {
    if (fullscreenElement() === stage) {
      if (document.exitFullscreen) await document.exitFullscreen();
      else if (document.webkitExitFullscreen) document.webkitExitFullscreen();
    } else {
      await enterStageFullscreen();
    }
  } catch (err) {
    setStatus(err.message);
  }
  updateFullscreenCaption();
});

function playbackStatus(result) {
  const track = trackLabel(selectedTrack);
  if (result.playing && result.mutedFallback) {
    return `Streaming muted · click video to unmute · ${track}`;
  }
  if (result.playing) {
    return `Streaming · ${track}`;
  }
  return `Ready · press play on this page once · ${track}`;
}

async function loadVideos() {
  const res = await fetch("/api/videos");
  if (!res.ok) throw new Error(await res.text());
  videos = await res.json();
}

function defaultTrack(video) {
  return (video?.audioTracks?.length || 0) > 1 ? 1 : 0;
}

async function playVideo(video, opts = {}) {
  current = video;
  selectedTrack = Number.isInteger(opts.track) ? opts.track : defaultTrack(video);
  nowPlaying.textContent = video.name;
  updateFullscreenCaption();
  destroyHls();
  setOverlay(true, "Transcoding with ffmpeg…");
  setStatus("Starting HLS transcode");
  reportState({ paused: true, playing: false, status: playbackStatusText });

  try {
    const res = await fetch(`/api/prepare?file=${encodeURIComponent(video.path)}`);
    const payload = await res.json();
    if (!res.ok) throw new Error(payload.error || "Prepare failed");
    const result = await startPlayback(payload.playlist);
    setOverlay(false);
    setStatus(playbackStatus(result));
    acceptEnd = true;
    updateFullscreenCaption();
    reportState();
  } catch (err) {
    setOverlay(true, "Could not start playback");
    setStatus(err.message);
    updateFullscreenCaption();
    reportState({ paused: true, playing: false });
  }
}

function startPlayback(playlistUrl) {
  return new Promise((resolve, reject) => {
    const onReady = () => {
      selectTrack(selectedTrack);
      tryAutoplay().then(resolve, () => resolve({ playing: false }));
    };

    if (Hls.isSupported()) {
      hls = new Hls({
        enableWorker: true,
        lowLatencyMode: false,
        startPosition: 0,
      });
      hls.loadSource(playlistUrl);
      hls.attachMedia(player);
      hls.on(Hls.Events.MANIFEST_PARSED, onReady);
      hls.on(Hls.Events.AUDIO_TRACKS_UPDATED, (_, data) => {
        if (data.audioTracks && data.audioTracks.length) {
          hls.audioTrack = selectedTrack;
        }
      });
      hls.on(Hls.Events.ERROR, (_, data) => {
        if (!data.fatal) return;
        reject(new Error(data.details || "HLS error"));
      });
      return;
    }

    if (player.canPlayType("application/vnd.apple.mpegurl")) {
      player.src = playlistUrl;
      player.addEventListener("loadedmetadata", onReady, { once: true });
      player.addEventListener(
        "error",
        () => reject(new Error("Native HLS playback failed")),
        { once: true }
      );
      return;
    }

    reject(new Error("HLS is not supported in this browser"));
  });
}

async function handlePlay(cmd) {
  const video = videos.find((v) => v.path === cmd.file);
  if (!video) {
    setStatus(`Unknown file ${cmd.file}`);
    return;
  }
  const track = Number.isInteger(cmd.track) ? cmd.track : defaultTrack(video);
  if (current && current.path === video.path && player.readyState >= 2) {
    selectedTrack = track;
    selectTrack(selectedTrack);
    const result = await tryAutoplay();
    setStatus(playbackStatus(result));
    updateFullscreenCaption();
    reportState();
    return;
  }
  await playVideo(video, { track });
}

async function handleResume() {
  if (!current) return;
  const result = await tryAutoplay();
  setStatus(playbackStatus(result));
  updateFullscreenCaption();
  reportState();
}

function handleIdle() {
  current = null;
  selectedTrack = 0;
  destroyHls();
  nowPlaying.textContent = "No song is currently playing. Scan the QR code to pick a song.";
  setStatus("");
  setOverlay(false);
  updateFullscreenCaption();
}

function handlePause() {
  player.pause();
  setStatus(`Paused · ${trackLabel(selectedTrack)}`);
  updateFullscreenCaption();
  reportState({ paused: true, playing: false });
}

function handleTrack(cmd) {
  if (!Number.isInteger(cmd.track)) return;
  selectTrack(cmd.track);
  setStatus(
    player.paused
      ? `Paused · ${trackLabel(selectedTrack)}`
      : `Streaming · ${trackLabel(selectedTrack)}`
  );
  updateFullscreenCaption();
  reportState();
}

function listenForCommands() {
  const es = new EventSource("/api/events");
  es.addEventListener("command", (ev) => {
    let cmd;
    try {
      cmd = JSON.parse(ev.data);
    } catch {
      return;
    }
    if (cmd.type === "state") {
      if (Array.isArray(cmd.queue)) upcoming = cmd.queue;
      updateFullscreenCaption();
      return;
    }
    switch (cmd.type) {
      case "play":
        if (cmd.file) handlePlay(cmd);
        break;
      case "pause":
        handlePause();
        break;
      case "resume":
        handleResume();
        break;
      case "track":
        handleTrack(cmd);
        break;
      case "idle":
        handleIdle();
        break;
      default:
        break;
    }
  });
}

player.addEventListener("pause", () => {
  if (current) reportState({ paused: true, playing: false });
});
player.addEventListener("play", () => {
  if (current) reportState({ paused: false, playing: true });
});
player.addEventListener("ended", () => {
  if (!acceptEnd) return;
  acceptEnd = false;
  fetch("/api/next", { method: "POST" }).catch(() => {});
});

updateFullscreenCaption();
fetch("/api/state")
  .then((res) => (res.ok ? res.json() : null))
  .then((state) => {
    if (state && Array.isArray(state.queue)) upcoming = state.queue;
    updateFullscreenCaption();
  })
  .catch(() => {});
loadVideos()
  .then(listenForCommands)
  .catch((err) => {
    setStatus(err.message);
  });
