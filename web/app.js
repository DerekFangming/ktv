const nowPlaying = document.getElementById("now-playing");
const player = document.getElementById("player");
const overlay = document.getElementById("overlay");
const overlayText = document.getElementById("overlay-text");
const stage = document.getElementById("stage");
const fsTitle = document.getElementById("fs-title");
const fsDetail = document.getElementById("fs-detail");
const fsToggle = document.getElementById("fs-toggle");
const ytSlot = document.getElementById("yt-slot");
const ytUnmute = document.getElementById("yt-unmute");

let ytPlayer = null;
let ytAPIPromise = null;
let ytToken = 0;

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

function destroyYouTube() {
  ytToken += 1;
  acceptEnd = false;
  if (ytPlayer && typeof ytPlayer.destroy === "function") {
    try {
      ytPlayer.destroy();
    } catch {
      // The iframe is already gone.
    }
  }
  ytPlayer = null;
  ytSlot.replaceChildren();
  ytSlot.classList.add("hidden");
  player.classList.remove("yt-hidden");
  ytUnmute.hidden = true;
}

function loadYouTubeAPI() {
  if (window.YT && window.YT.Player) return Promise.resolve();
  if (!ytAPIPromise) {
    ytAPIPromise = new Promise((resolve) => {
      const previous = window.onYouTubeIframeAPIReady;
      window.onYouTubeIframeAPIReady = () => {
        if (typeof previous === "function") previous();
        resolve();
      };
      const script = document.createElement("script");
      script.src = "https://www.youtube.com/iframe_api";
      document.head.appendChild(script);
    });
  }
  return ytAPIPromise;
}

let namedHost = "";

function hostIsIP(host) {
  return host.includes(":") || /^\d{1,3}(\.\d{1,3}){3}$/.test(host);
}

function namedPlayerURL() {
  if (!namedHost) return "";
  const port = location.port ? `:${location.port}` : "";
  return `${location.protocol}//${namedHost}${port}/`;
}

function youtubeErrorMessage(code) {
  if ((code === 101 || code === 150) && hostIsIP(location.hostname)) {
    const where = namedPlayerURL();
    return where
      ? `YouTube will not play from an IP address. Open ${where}`
      : "YouTube will not play from an IP address. Open this page by a hostname.";
  }
  if (code === 101 || code === 150) return "YouTube does not allow this video to play here";
  if (code === 100) return "This YouTube video is unavailable";
  if (code === 153) return "YouTube blocked playback from this page";
  return "Could not start playback";
}

async function useNamedHost() {
  if (!hostIsIP(location.hostname)) return false;
  let data;
  try {
    const res = await fetch("/api/playback-host");
    if (!res.ok) return false;
    data = await res.json();
  } catch {
    return false;
  }
  if (!data.host) return false;
  namedHost = data.host;
  const target = namedPlayerURL();
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), 1500);
  try {
    const probe = await fetch(`${target}api/playback-host`, { signal: ctrl.signal });
    if (!probe.ok) return false;
  } catch {
    return false;
  } finally {
    clearTimeout(timer);
  }
  location.replace(target);
  return true;
}

function playYouTube(video) {
  const videoId = video.videoId || String(video.path).slice("youtube:".length);
  destroyYouTube();
  const token = ytToken;
  destroyHls();
  current = { path: video.path, name: video.name, videoId, youtube: true, audioTracks: [] };
  selectedTrack = 0;
  nowPlaying.textContent = video.name;
  updateFullscreenCaption();
  player.classList.add("yt-hidden");
  ytSlot.classList.remove("hidden");
  setOverlay(true, "Opening YouTube…");
  setStatus("Opening YouTube");
  reportState({ paused: true, playing: false, tracks: [], status: playbackStatusText });

  // YouTube shows "This video is unavailable" (error 153) when the embed
  // request has no Referer. Build the iframe first so that policy is set
  // before the player loads. The IFrame API's own iframe omits it.
  const params = new URLSearchParams({
    autoplay: "1",
    enablejsapi: "1",
    mute: "1",
    modestbranding: "1",
    origin: location.origin,
    playsinline: "1",
    rel: "0",
  });
  const frame = document.createElement("iframe");
  frame.id = "yt-player";
  frame.title = video.name || "YouTube";
  frame.allow = "autoplay; encrypted-media; picture-in-picture; fullscreen";
  frame.referrerPolicy = "strict-origin-when-cross-origin";
  frame.src = "https://www.youtube.com/embed/" + encodeURIComponent(videoId) + "?" + params;
  ytSlot.appendChild(frame);

  return loadYouTubeAPI()
    .then(
      () =>
        new Promise((resolve) => {
          ytPlayer = new YT.Player(frame, {
            events: {
              onReady: (event) => {
                if (token !== ytToken) return;
                event.target.playVideo();
                event.target.unMute();
                event.target.setVolume(100);
                setOverlay(false);
                acceptEnd = true;
                updateFullscreenCaption();
                window.setTimeout(() => {
                  if (token !== ytToken) return;
                  const state = event.target.getPlayerState();
                  const moving = state === YT.PlayerState.PLAYING || state === YT.PlayerState.BUFFERING;
                  if (!moving || event.target.isMuted()) {
                    event.target.mute();
                    event.target.playVideo();
                    ytUnmute.hidden = false;
                    setStatus("Streaming muted · click Unmute");
                  } else {
                    ytUnmute.hidden = true;
                    setStatus("Streaming");
                  }
                  reportState({ paused: false, playing: true, tracks: [], status: playbackStatusText });
                }, 400);
                resolve();
              },
              onError: (event) => {
                if (token !== ytToken) return;
                const message = youtubeErrorMessage(event.data);
                ytSlot.classList.add("hidden");
                setOverlay(true, `${message} · skipping`);
                setStatus(message);
                reportState({ paused: true, playing: false, tracks: [] });
                const blockedIP = (event.data === 101 || event.data === 150) && hostIsIP(location.hostname);
                if (!blockedIP) {
                  window.setTimeout(() => {
                    if (token !== ytToken) return;
                    fetch("/api/next", { method: "POST" }).catch(() => {});
                  }, 3000);
                }
                resolve();
              },
              onStateChange: (event) => {
                if (token !== ytToken || !current?.youtube) return;
                if (event.data === YT.PlayerState.ENDED) {
                  if (!acceptEnd) return;
                  acceptEnd = false;
                  fetch("/api/next", { method: "POST" }).catch(() => {});
                  return;
                }
                if (event.data === YT.PlayerState.PLAYING) {
                  const muted = event.target.isMuted?.();
                  ytUnmute.hidden = !muted;
                  setStatus(muted ? "Streaming muted · click Unmute" : "Streaming");
                  reportState({ paused: false, playing: true, tracks: [], status: playbackStatusText });
                } else if (event.data === YT.PlayerState.PAUSED) {
                  setStatus("Paused");
                  reportState({ paused: true, playing: false, tracks: [] });
                }
              },
            },
          });
        })
    )
    .catch((err) => {
      setOverlay(true, "Could not start playback");
      setStatus(err.message);
      reportState({ paused: true, playing: false, tracks: [] });
    });
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

function playbackTracks() {
  if (hls && hls.audioTracks && hls.audioTracks.length) {
    return Array.from(hls.audioTracks).map((t, i) => t.name || `Track ${i + 1}`);
  }
  const native = player.audioTracks;
  if (native && native.length) {
    return Array.from(native).map((t, i) => t.label || t.language || `Track ${i + 1}`);
  }
  return trackNames(current);
}

function reportState(extra = {}) {
  const body = {
    file: current?.path || "",
    name: current?.name || "",
    track: selectedTrack,
    paused: player.paused,
    playing: !player.paused && player.readyState > 2,
    status: playbackStatusText,
    tracks: playbackTracks(),
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
  return document.fullscreenElement || document.webkitFullscreenElement || document.mozFullScreenElement || null;
}

function nextSongLine() {
  const next = upcoming[0];
  if (next) return `Next song: ${next.name || next.file}`;
  return "No next song. Scan the QR code to choose one.";
}

function updateFullscreenCaption() {
  const onStage = fullscreenElement() === stage;
  stage.classList.toggle("is-fullscreen", onStage);
  fsTitle.textContent = current?.name || nowPlaying.textContent;
  fsDetail.textContent = current ? nextSongLine() : "";
  fsToggle.textContent = onStage ? "Exit fullscreen" : "Fullscreen";
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
      await enterStageFullscreen();
    } catch {
      // Firefox will not move fullscreen off the video element without a new click.
    } finally {
      movingFullscreen = false;
    }
  }
  updateFullscreenCaption();
}

document.addEventListener("fullscreenchange", onFullscreenChange);
document.addEventListener("webkitfullscreenchange", onFullscreenChange);
document.addEventListener("mozfullscreenchange", onFullscreenChange);
ytUnmute.addEventListener("click", () => {
  if (!ytPlayer || typeof ytPlayer.unMute !== "function") return;
  ytPlayer.unMute();
  ytPlayer.setVolume(100);
  ytPlayer.playVideo();
  ytUnmute.hidden = true;
  setStatus("Streaming");
  reportState({ paused: false, playing: true, tracks: [], status: playbackStatusText });
});

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
  const res = await fetch("/api/videos?limit=20");
  if (!res.ok) throw new Error(await res.text());
  const page = await res.json();
  videos = Array.isArray(page.songs) ? page.songs : [];
}

function defaultTrack(video) {
  return (video?.audioTracks?.length || 0) > 1 ? 1 : 0;
}

async function playVideo(video, opts = {}) {
  destroyYouTube();
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
        reportState();
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
  if (typeof cmd.file === "string" && cmd.file.startsWith("youtube:")) {
    if (current?.youtube && current.path === cmd.file && ytPlayer && typeof ytPlayer.playVideo === "function") {
      ytPlayer.playVideo();
      return;
    }
    await playYouTube({ path: cmd.file, name: cmd.name || cmd.file });
    return;
  }
  let video = videos.find((v) => v.path === cmd.file);
  if (!video) {
    try {
      await loadVideos();
    } catch {
      // The catalog request failed; fall through and play by path.
    }
    video = videos.find((v) => v.path === cmd.file);
  }
  if (!video) {
    video = { path: cmd.file, name: cmd.name || cmd.file, audioTracks: [] };
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
  if (current.youtube && ytPlayer && typeof ytPlayer.playVideo === "function") {
    ytPlayer.playVideo();
    setStatus("Streaming");
    reportState({ paused: false, playing: true, tracks: [] });
    return;
  }
  const result = await tryAutoplay();
  setStatus(playbackStatus(result));
  updateFullscreenCaption();
  reportState();
}

function handleIdle() {
  current = null;
  selectedTrack = 0;
  destroyYouTube();
  destroyHls();
  nowPlaying.textContent = "No song is currently playing. Scan the QR code to pick a song.";
  setStatus("");
  setOverlay(false);
  updateFullscreenCaption();
}

function handlePause() {
  if (current?.youtube && ytPlayer && typeof ytPlayer.pauseVideo === "function") {
    ytPlayer.pauseVideo();
    setStatus("Paused");
    updateFullscreenCaption();
    reportState({ paused: true, playing: false, tracks: [] });
    return;
  }
  player.pause();
  setStatus(`Paused · ${trackLabel(selectedTrack)}`);
  updateFullscreenCaption();
  reportState({ paused: true, playing: false });
}

function handleSeek(cmd) {
  if (!current || !Number.isInteger(cmd.delta) || cmd.delta === 0) return;
  if (current.youtube && ytPlayer && typeof ytPlayer.getCurrentTime === "function") {
    let next = ytPlayer.getCurrentTime() + cmd.delta;
    if (next < 0) next = 0;
    const duration = ytPlayer.getDuration();
    if (Number.isFinite(duration) && duration > 0 && next > duration) next = Math.max(0, duration - 0.25);
    ytPlayer.seekTo(next, true);
    return;
  }
  const duration = player.duration;
  let next = (player.currentTime || 0) + cmd.delta;
  if (next < 0) next = 0;
  if (Number.isFinite(duration) && next > duration) next = Math.max(0, duration - 0.25);
  player.currentTime = next;
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
      case "seek":
        handleSeek(cmd);
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
  if (current && !current.youtube) reportState({ paused: true, playing: false });
});
player.addEventListener("play", () => {
  if (current && !current.youtube) reportState({ paused: false, playing: true });
});
player.addEventListener("ended", () => {
  if (!acceptEnd) return;
  acceptEnd = false;
  fetch("/api/next", { method: "POST" }).catch(() => {});
});

useNamedHost()
  .then((leaving) => {
    if (leaving) return;
    updateFullscreenCaption();
    fetch("/api/state")
      .then((res) => (res.ok ? res.json() : null))
      .then((state) => {
        if (state && Array.isArray(state.queue)) upcoming = state.queue;
        updateFullscreenCaption();
      })
      .catch(() => {});
    return loadVideos().then(listenForCommands);
  })
  .catch((err) => {
    setStatus(err.message);
  });
