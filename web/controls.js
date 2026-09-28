const searchEl = document.getElementById("song-search");
const listEl = document.getElementById("video-list");
const listStatus = document.getElementById("list-status");
const queueEl = document.getElementById("queue-list");
const queueStatus = document.getElementById("queue-status");
const queueCount = document.getElementById("queue-count");
const nowPlaying = document.getElementById("now-playing");
const tracksEl = document.getElementById("tracks");
const statusEl = document.getElementById("status");
const pauseBtn = document.getElementById("pause-btn");
const resumeBtn = document.getElementById("resume-btn");
const nextBtn = document.getElementById("next-btn");

let videos = [];
let state = {
  file: "",
  name: "",
  track: 0,
  paused: true,
  playing: false,
  status: "",
  tracks: [],
  queue: [],
};

async function postCommand(path) {
  const res = await fetch(path, { method: "POST" });
  const payload = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(payload.error || res.statusText);
  return payload;
}

function iconButton(src, label) {
  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = "action icon";
  btn.setAttribute("aria-label", label);
  btn.title = label;
  const img = document.createElement("img");
  img.src = src;
  img.alt = "";
  btn.appendChild(img);
  return btn;
}

function songLine(video) {
  return [video?.singer, video?.language, video?.style].filter(Boolean).join(" · ");
}

function applyState(next) {
  state = {
    ...state,
    ...next,
    queue: Array.isArray(next.queue) ? next.queue : state.queue,
  };
  const video = videos.find((v) => v.path === state.file);
  nowPlaying.textContent = state.name || video?.name || "Nothing playing";
  statusEl.textContent = state.status || "";
  const hasFile = Boolean(state.file);
  pauseBtn.disabled = !hasFile || state.paused;
  resumeBtn.disabled = !hasFile || !state.paused;
  const waiting = state.queue.length;
  queueCount.textContent = waiting ? String(waiting) : "";
  renderQueue();
  renderTracks(video);
}

function renderQueue() {
  queueEl.innerHTML = "";
  if (!state.file && !state.queue.length) {
    queueStatus.textContent = "The queue is empty. Add songs from the Songs tab.";
    return;
  }
  queueStatus.textContent = "";
  if (state.file) {
    const li = document.createElement("li");
    li.className = "active";
    const title = document.createElement("span");
    title.className = "title";
    title.textContent = state.name || state.file;
    const sub = document.createElement("span");
    sub.className = "sub";
    sub.textContent = "Now playing";
    li.append(title, sub);
    queueEl.appendChild(li);
  }
  state.queue.forEach((item, index) => {
    const li = document.createElement("li");
    const title = document.createElement("span");
    title.className = "title";
    title.textContent = item.name || item.file;
    const sub = document.createElement("span");
    sub.className = "sub";
    sub.textContent = `Up next · ${index + 1}`;
    const actions = document.createElement("div");
    actions.className = "row-actions";
    const top = iconButton("/static/icons/top.png", "Play next");
    top.disabled = index === 0;
    top.addEventListener("click", async (ev) => {
      ev.stopPropagation();
      try {
        await postCommand(`/api/queue/top?id=${encodeURIComponent(item.id)}`);
      } catch (err) {
        queueStatus.textContent = err.message;
      }
    });
    const remove = iconButton("/static/icons/trash.png", "Remove");
    remove.addEventListener("click", async (ev) => {
      ev.stopPropagation();
      try {
        await postCommand(`/api/queue/remove?id=${encodeURIComponent(item.id)}`);
      } catch (err) {
        queueStatus.textContent = err.message;
      }
    });
    actions.append(top, remove);
    li.append(title, sub, actions);
    queueEl.appendChild(li);
  });
}

function trackButtonLabel(index) {
  if (index === 0) return "原唱";
  if (index === 1) return "伴奏";
  return `Track ${index + 1}`;
}

function renderTracks(video) {
  tracksEl.innerHTML = "";
  const reported = Math.max(state.tracks?.length || 0, video?.audioTracks?.length || 0);
  const count = reported || (state.file ? 2 : 0);
  if (!count) {
    const empty = document.createElement("p");
    empty.className = "muted";
    empty.textContent = "A soundtrack appears here while a song is playing.";
    tracksEl.appendChild(empty);
    return;
  }
  const order = [];
  if (count > 1) order.push(1);
  order.push(0);
  for (let i = 2; i < count; i++) order.push(i);
  order.forEach((i) => {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "track" + (i === state.track ? " active" : "");
    btn.textContent = trackButtonLabel(i);
    btn.disabled = !state.file;
    btn.addEventListener("click", async () => {
      try {
        await postCommand(`/api/track?track=${i}`);
      } catch (err) {
        statusEl.textContent = err.message;
      }
    });
    tracksEl.appendChild(btn);
  });
}

function renderSongs(query) {
  listEl.innerHTML = "";
  if (!videos.length) {
    listStatus.textContent = query ? "No matching songs" : "No songs loaded";
    return;
  }
  listStatus.textContent = `${videos.length} song${videos.length === 1 ? "" : "s"}`;
  videos.forEach((v) => {
    const li = document.createElement("li");
    const title = document.createElement("span");
    title.className = "title";
    title.textContent = v.song || v.name;
    const sub = document.createElement("span");
    sub.className = "sub";
    sub.textContent = songLine(v);
    const add = iconButton("/static/icons/plus.png", "Add");
    add.addEventListener("click", (ev) => {
      ev.stopPropagation();
      addSong(v);
    });
    li.addEventListener("click", () => addSong(v));
    li.append(title, sub, add);
    listEl.appendChild(li);
  });
}

let searchSeq = 0;

async function loadList() {
  const seq = ++searchSeq;
  const query = searchEl.value.trim();
  listStatus.textContent = "Loading…";
  const res = await fetch(`/api/videos?q=${encodeURIComponent(query)}`);
  if (seq !== searchSeq) return;
  if (!res.ok) throw new Error(await res.text());
  videos = await res.json();
  renderSongs(query);
}

let searchTimer;
searchEl.addEventListener("input", () => {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    loadList().catch((err) => {
      listStatus.textContent = err.message;
    });
  }, 200);
});

async function addSong(video) {
  try {
    const result = await postCommand(`/api/queue?file=${encodeURIComponent(video.path)}`);
    listStatus.textContent = result.started ? `Playing ${video.name}` : `Added ${video.name}`;
  } catch (err) {
    listStatus.textContent = err.message;
  }
}

function showTab(name) {
  document.querySelectorAll(".tab-panel").forEach((panel) => {
    const on = panel.dataset.tab === name;
    panel.hidden = !on;
    panel.classList.toggle("active", on);
  });
  document.querySelectorAll(".tabbar .tab").forEach((btn) => {
    btn.classList.toggle("active", btn.dataset.tab === name);
  });
  sessionStorage.setItem("controls-tab", name);
}

document.querySelectorAll(".tabbar .tab").forEach((btn) => {
  btn.addEventListener("click", () => showTab(btn.dataset.tab));
});

pauseBtn.addEventListener("click", async () => {
  try {
    await postCommand("/api/pause");
  } catch (err) {
    statusEl.textContent = err.message;
  }
});

resumeBtn.addEventListener("click", async () => {
  try {
    await postCommand("/api/resume");
  } catch (err) {
    statusEl.textContent = err.message;
  }
});

nextBtn.addEventListener("click", async () => {
  try {
    await postCommand("/api/next");
  } catch (err) {
    statusEl.textContent = err.message;
  }
});

function listenForState() {
  const es = new EventSource("/api/events");
  es.addEventListener("command", (ev) => {
    let cmd;
    try {
      cmd = JSON.parse(ev.data);
    } catch {
      return;
    }
    if (cmd.type === "state") applyState(cmd);
  });
}

async function init() {
  try {
    await loadList();
    const res = await fetch("/api/state");
    if (res.ok) applyState(await res.json());
    listenForState();
    showTab(sessionStorage.getItem("controls-tab") || "songs");
  } catch (err) {
    listStatus.textContent = "Failed to load library";
    statusEl.textContent = err.message;
  }
}

init();
