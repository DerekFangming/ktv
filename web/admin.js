const loadBtn = document.getElementById("load-btn");
const clearBtn = document.getElementById("clear-btn");
const statusEl = document.getElementById("admin-status");

function setBusy(busy) {
  loadBtn.disabled = busy;
  clearBtn.disabled = busy;
}

async function post(path) {
  const res = await fetch(path, { method: "POST" });
  const payload = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(payload.error || res.statusText);
  return payload;
}

function describeLoad(result) {
  const lines = [
    `Added ${result.added}.`,
    `Skipped ${result.skippedDuplicate} already loaded.`,
    `Skipped ${result.skippedInvalid} invalid names.`,
  ];
  if (Array.isArray(result.invalid) && result.invalid.length) {
    lines.push(`Invalid: ${result.invalid.join(", ")}`);
  }
  return lines.join(" ");
}

loadBtn.addEventListener("click", async () => {
  setBusy(true);
  statusEl.textContent = "Scanning…";
  try {
    const result = await post("/api/admin/load");
    statusEl.textContent = describeLoad(result);
  } catch (err) {
    statusEl.textContent = err.message;
  } finally {
    setBusy(false);
  }
});

clearBtn.addEventListener("click", async () => {
  if (!window.confirm("Clear every song from the library?")) return;
  setBusy(true);
  statusEl.textContent = "Clearing…";
  try {
    const result = await post("/api/admin/clear");
    statusEl.textContent = `Cleared ${result.cleared} songs.`;
  } catch (err) {
    statusEl.textContent = err.message;
  } finally {
    setBusy(false);
  }
});
