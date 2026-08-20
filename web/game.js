(() => {
  "use strict";

  const lobby = document.querySelector("#lobby");
  const battle = document.querySelector("#battle");
  const soloButton = document.querySelector("#soloButton");
  const createButton = document.querySelector("#createButton");
  const joinForm = document.querySelector("#joinForm");
  const roomCodeInput = document.querySelector("#roomCode");
  const lobbyStatus = document.querySelector("#lobbyStatus");
  const battleStatus = document.querySelector("#battleStatus");
  const roomCodeButton = document.querySelector("#copyCode");
  const waitingCode = document.querySelector("#waitingCode");
  const waitingOverlay = document.querySelector("#waitingOverlay");
  const resultOverlay = document.querySelector("#resultOverlay");
  const resultTitle = document.querySelector("#resultTitle");
  const resultReason = document.querySelector("#resultReason");
  const backToLobby = document.querySelector("#backToLobby");
  const resourcesText = document.querySelector("#resources");
  const phaseText = document.querySelector("#phaseText");
  const timerText = document.querySelector("#timer");
  const sideText = document.querySelector("#sideText");
  const unitButtons = document.querySelector("#unitButtons");
  const canvas = document.querySelector("#gameCanvas");
  const ctx = canvas.getContext("2d");

  const unitOrder = ["rifleman", "assault", "gunner", "tank"];
  const state = {
    socket: null,
    code: "",
    mode: "",
    side: "allies",
    catalog: {},
    match: null,
    displayUnits: new Map(),
    attackPulse: new Map(),
    effects: [],
    connected: false,
  };

  function connect(mode, code = "") {
    closeSocket();
    lobbyStatus.textContent = "Menghubungkan ke medan tempur…";
    const scheme = location.protocol === "https:" ? "wss" : "ws";
    const params = new URLSearchParams({ mode });
    if (code) params.set("code", code.trim().toUpperCase());
    const socket = new WebSocket(`${scheme}://${location.host}/ws?${params}`);
    state.socket = socket;

    socket.addEventListener("open", () => {
      state.connected = true;
      lobbyStatus.textContent = "";
    });

    socket.addEventListener("message", (event) => {
      let message;
      try {
        message = JSON.parse(event.data);
      } catch {
        setBattleStatus("Data server tidak dapat dibaca.");
        return;
      }
      handleMessage(message);
    });

    socket.addEventListener("close", () => {
      state.connected = false;
      if (!battle.classList.contains("hidden") && state.match?.phase !== "finished") {
        setBattleStatus("Koneksi terputus. Kembali ke lobby untuk mencoba lagi.");
      }
    });

    socket.addEventListener("error", () => {
      lobbyStatus.textContent = "Gagal tersambung. Pastikan server berjalan.";
    });
  }

  function handleMessage(message) {
    switch (message.type) {
      case "welcome":
        state.code = message.code;
        state.mode = message.mode;
        state.side = message.side;
        state.catalog = message.catalog || {};
        setupBattle();
        break;
      case "state":
        if (message.state) {
          state.match = message.state;
          syncEffects(message.state.units || []);
          updateHUD();
        }
        break;
      case "notice":
        setBattleStatus(message.message || "");
        window.setTimeout(() => setBattleStatus(""), 2600);
        break;
      case "error":
        if (battle.classList.contains("hidden")) {
          lobbyStatus.textContent = message.message || "Terjadi kesalahan.";
        } else {
          setBattleStatus(message.message || "Perintah ditolak server.");
        }
        break;
      default:
        break;
    }
  }

  function setupBattle() {
    lobby.classList.add("hidden");
    battle.classList.remove("hidden");
    roomCodeButton.textContent = state.mode === "solo" ? "SOLO" : state.code;
    waitingCode.textContent = state.code;
    sideText.textContent = state.side.toUpperCase();
    sideText.style.color = state.side === "allies" ? "#8ab7d7" : "#d78a76";
    buildUnitButtons();
    resizeCanvas();
  }

  function buildUnitButtons() {
    unitButtons.replaceChildren();
    unitOrder.forEach((unitType, index) => {
      const spec = state.catalog[unitType];
      if (!spec) return;
      const button = document.createElement("button");
      button.type = "button";
      button.className = "unit-button";
      button.dataset.unit = unitType;
      button.title = spec.description || spec.name;
      button.innerHTML = `<strong>${index + 1}. ${escapeHTML(spec.name)}</strong><span>${Math.round(spec.cost)} supply</span>`;
      button.addEventListener("click", () => spawn(unitType));
      unitButtons.append(button);
    });
  }

  function spawn(unitType) {
    if (!state.socket || state.socket.readyState !== WebSocket.OPEN || state.match?.phase !== "playing") return;
    state.socket.send(JSON.stringify({ type: "spawn", unit: unitType }));
  }

  function updateHUD() {
    const match = state.match;
    if (!match) return;
    const player = match.players?.[state.side];
    resourcesText.textContent = player ? Math.floor(player.resources) : "0";
    timerText.textContent = formatTime(match.elapsed || 0);
    phaseText.textContent = phaseLabel(match.phase);

    waitingOverlay.classList.toggle("hidden", !(match.phase === "waiting" && state.mode === "pvp"));
    resultOverlay.classList.toggle("hidden", match.phase !== "finished");

    if (match.phase === "finished") {
      const won = match.winner === state.side;
      const draw = match.winner === "draw";
      resultTitle.textContent = draw ? "Seri" : won ? "Kemenangan" : "Kekalahan";
      resultReason.textContent = humanReason(match.reason);
    }

    document.querySelectorAll(".unit-button").forEach((button) => {
      const spec = state.catalog[button.dataset.unit];
      button.disabled = match.phase !== "playing" || !player || player.resources < spec.cost;
    });
  }

  function syncEffects(units) {
    const live = new Set();
    units.forEach((unit) => {
      live.add(unit.id);
      const previousPulse = state.attackPulse.get(unit.id) || 0;
      if (unit.attackPulse > previousPulse) {
        state.effects.push({ x: unit.x, y: unit.y, side: unit.side, life: 0.13, maxLife: 0.13 });
      }
      state.attackPulse.set(unit.id, unit.attackPulse);
      if (!state.displayUnits.has(unit.id)) {
        state.displayUnits.set(unit.id, { x: unit.x, y: unit.y });
      }
    });
    [...state.displayUnits.keys()].forEach((id) => {
      if (!live.has(id)) {
        state.displayUnits.delete(id);
        state.attackPulse.delete(id);
      }
    });
  }

  function renderFrame(timestamp) {
    const dt = Math.min(0.05, (timestamp - (renderFrame.last || timestamp)) / 1000);
    renderFrame.last = timestamp;
    drawGame(dt);
    requestAnimationFrame(renderFrame);
  }

  function drawGame(dt) {
    const width = canvas.clientWidth;
    const height = canvas.clientHeight;
    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    if (canvas.width !== Math.round(width * dpr) || canvas.height !== Math.round(height * dpr)) {
      canvas.width = Math.round(width * dpr);
      canvas.height = Math.round(height * dpr);
    }
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

    drawBackground(width, height);
    const match = state.match;
    if (!match) {
      drawCenteredText("Menghubungkan…", width, height);
      return;
    }

    const scaleX = width / match.width;
    const scaleY = height / match.height;
    const groundY = height * 0.61;
    drawTrenches(match.trenches || [], scaleX, groundY, height);
    drawBases(match, scaleX, scaleY, groundY, width);

    const units = [...(match.units || [])].sort((a, b) => a.y - b.y);
    units.forEach((unit) => {
      const visual = state.displayUnits.get(unit.id) || { x: unit.x, y: unit.y };
      const smoothing = 1 - Math.pow(0.0001, dt);
      visual.x += (unit.x - visual.x) * smoothing;
      visual.y += (unit.y - visual.y) * smoothing;
      state.displayUnits.set(unit.id, visual);
      drawUnit(unit, visual.x * scaleX, visual.y * scaleY, scaleX, scaleY);
    });

    state.effects = state.effects.filter((effect) => {
      effect.life -= dt;
      if (effect.life <= 0) return false;
      drawMuzzleFlash(effect, scaleX, scaleY);
      return true;
    });
  }

  function drawBackground(width, height) {
    const sky = ctx.createLinearGradient(0, 0, 0, height);
    sky.addColorStop(0, "#777967");
    sky.addColorStop(0.58, "#a29b79");
    sky.addColorStop(0.59, "#625944");
    sky.addColorStop(1, "#29271e");
    ctx.fillStyle = sky;
    ctx.fillRect(0, 0, width, height);

    ctx.fillStyle = "rgba(48, 47, 39, 0.32)";
    for (let i = 0; i < 9; i += 1) {
      const x = (i * 193 + 70) % width;
      const y = height * (0.16 + (i % 3) * 0.08);
      ctx.beginPath();
      ctx.ellipse(x, y, 70 + (i % 2) * 30, 20, 0, 0, Math.PI * 2);
      ctx.fill();
    }

    ctx.strokeStyle = "rgba(20, 22, 17, 0.35)";
    ctx.lineWidth = 2;
    for (let y = height * 0.66; y < height; y += 28) {
      ctx.beginPath();
      ctx.moveTo(0, y);
      ctx.lineTo(width, y + 5 * Math.sin(y));
      ctx.stroke();
    }
  }

  function drawTrenches(trenches, scaleX, groundY, height) {
    trenches.forEach((trench) => {
      const x = trench.x * scaleX;
      const w = Math.max(18, trench.width * scaleX);
      ctx.fillStyle = "#171812";
      ctx.fillRect(x - w / 2, groundY - 6, w, height - groundY + 6);
      ctx.fillStyle = "#423b2c";
      ctx.fillRect(x - w / 2 - 4, groundY - 10, w + 8, 8);
      ctx.strokeStyle = "rgba(214, 169, 75, 0.22)";
      ctx.setLineDash([5, 7]);
      ctx.strokeRect(x - w / 2, groundY + 10, w, Math.max(30, height - groundY - 28));
      ctx.setLineDash([]);
    });
  }

  function drawBases(match, scaleX, scaleY, groundY, width) {
    drawBase("allies", 72 * scaleX, groundY, match.players.allies, false);
    drawBase("axis", width - 72 * scaleX, groundY, match.players.axis, true);
  }

  function drawBase(side, x, groundY, player, flipped) {
    const color = side === "allies" ? "#557d9b" : "#9c5949";
    ctx.save();
    ctx.translate(x, groundY);
    ctx.scale(flipped ? -1 : 1, 1);
    ctx.fillStyle = "#292a22";
    ctx.fillRect(-32, -88, 64, 88);
    ctx.fillStyle = color;
    ctx.fillRect(-36, -70, 72, 13);
    ctx.fillStyle = "#171812";
    ctx.fillRect(-15, -42, 30, 42);
    ctx.fillStyle = color;
    ctx.beginPath();
    ctx.moveTo(4, -88);
    ctx.lineTo(4, -145);
    ctx.lineTo(52, -126);
    ctx.lineTo(4, -109);
    ctx.closePath();
    ctx.fill();
    ctx.restore();
    drawHealthBar(x - 38, groundY - 165, 76, player.baseHp / player.maxBaseHp, color);
  }

  function drawUnit(unit, x, y, scaleX, scaleY) {
    const spec = state.catalog[unit.type] || {};
    const direction = unit.side === "allies" ? 1 : -1;
    const color = unit.side === "allies" ? "#5b84a6" : "#a75f4e";
    const radius = Math.max(8, (unit.radius || 14) * Math.min(scaleX, scaleY) * 1.25);

    ctx.save();
    ctx.translate(x, y);
    ctx.scale(direction, 1);

    if (unit.type === "tank") {
      ctx.fillStyle = "#2a2d23";
      ctx.fillRect(-radius * 1.2, -radius * 0.25, radius * 2.4, radius * 0.82);
      ctx.strokeStyle = "#11130f";
      ctx.lineWidth = 4;
      ctx.stroke();
      ctx.fillStyle = color;
      ctx.fillRect(-radius * 0.55, -radius * 0.7, radius * 1.2, radius * 0.55);
      ctx.fillStyle = "#1c1e18";
      ctx.fillRect(radius * 0.3, -radius * 0.6, radius * 1.3, radius * 0.12);
      ctx.fillStyle = "#11130f";
      for (let i = -2; i <= 2; i += 1) {
        ctx.beginPath();
        ctx.arc(i * radius * 0.38, radius * 0.42, radius * 0.22, 0, Math.PI * 2);
        ctx.fill();
      }
    } else {
      ctx.strokeStyle = "#1b1c17";
      ctx.lineWidth = Math.max(2, radius * 0.18);
      ctx.beginPath();
      ctx.moveTo(0, radius * 0.1);
      ctx.lineTo(-radius * 0.25, radius * 1.1);
      ctx.moveTo(0, radius * 0.1);
      ctx.lineTo(radius * 0.42, radius * 1.05);
      ctx.stroke();
      ctx.fillStyle = color;
      ctx.fillRect(-radius * 0.55, -radius * 0.55, radius * 1.1, radius * 1.05);
      ctx.fillStyle = "#c7b28b";
      ctx.beginPath();
      ctx.arc(0, -radius * 0.9, radius * 0.43, 0, Math.PI * 2);
      ctx.fill();
      ctx.fillStyle = "#292b22";
      ctx.fillRect(-radius * 0.47, -radius * 1.2, radius * 0.94, radius * 0.28);
      ctx.strokeStyle = "#1b1c17";
      ctx.lineWidth = Math.max(2, radius * 0.18);
      ctx.beginPath();
      ctx.moveTo(radius * 0.2, -radius * 0.3);
      ctx.lineTo(radius * (unit.type === "gunner" ? 1.75 : 1.35), -radius * 0.2);
      ctx.stroke();
      if (unit.type === "assault") {
        ctx.fillStyle = "#d6a94b";
        ctx.fillRect(-radius * 0.18, -radius * 0.28, radius * 0.35, radius * 0.35);
      }
    }
    ctx.restore();

    drawHealthBar(x - radius, y - radius * 1.8, radius * 2, unit.hp / unit.maxHp, color);
    if (spec.name) {
      ctx.fillStyle = "rgba(243, 236, 215, 0.68)";
      ctx.font = "10px system-ui";
      ctx.textAlign = "center";
      ctx.fillText(spec.name, x, y + radius * 1.65);
    }
  }

  function drawHealthBar(x, y, width, ratio, color) {
    const clamped = Math.max(0, Math.min(1, ratio));
    ctx.fillStyle = "rgba(12, 13, 10, 0.72)";
    ctx.fillRect(x, y, width, 5);
    ctx.fillStyle = color;
    ctx.fillRect(x + 1, y + 1, Math.max(0, (width - 2) * clamped), 3);
  }

  function drawMuzzleFlash(effect, scaleX, scaleY) {
    const x = effect.x * scaleX + (effect.side === "allies" ? 18 : -18);
    const y = effect.y * scaleY - 8;
    const alpha = effect.life / effect.maxLife;
    ctx.save();
    ctx.globalAlpha = alpha;
    ctx.fillStyle = "#ffd67a";
    ctx.beginPath();
    ctx.arc(x, y, 4 + (1 - alpha) * 7, 0, Math.PI * 2);
    ctx.fill();
    ctx.restore();
  }

  function drawCenteredText(text, width, height) {
    ctx.fillStyle = "rgba(243, 236, 215, 0.8)";
    ctx.font = "700 20px system-ui";
    ctx.textAlign = "center";
    ctx.fillText(text, width / 2, height / 2);
  }

  function resizeCanvas() {
    const frame = canvas.parentElement;
    const available = Math.max(360, window.innerHeight - 210);
    frame.style.height = `${available}px`;
  }

  function closeSocket() {
    if (state.socket) {
      state.socket.close();
      state.socket = null;
    }
  }

  function returnToLobby() {
    closeSocket();
    state.match = null;
    state.displayUnits.clear();
    state.attackPulse.clear();
    state.effects = [];
    resultOverlay.classList.add("hidden");
    waitingOverlay.classList.add("hidden");
    battle.classList.add("hidden");
    lobby.classList.remove("hidden");
    lobbyStatus.textContent = "";
    battleStatus.textContent = "";
  }

  function setBattleStatus(text) {
    battleStatus.textContent = text;
  }

  function phaseLabel(phase) {
    if (phase === "playing") return "BERTEMPUR";
    if (phase === "finished") return "SELESAI";
    return "MENUNGGU";
  }

  function humanReason(reason) {
    const reasons = {
      "axis headquarters destroyed": "Markas Axis berhasil dihancurkan.",
      "allied headquarters destroyed": "Markas Allies berhasil dihancurkan.",
      "both headquarters were destroyed": "Kedua markas hancur pada saat bersamaan.",
      "opponent disconnected": "Lawan meninggalkan room.",
    };
    return reasons[reason] || reason || "Pertempuran telah berakhir.";
  }

  function formatTime(seconds) {
    const total = Math.max(0, Math.floor(seconds));
    const minutes = String(Math.floor(total / 60)).padStart(2, "0");
    const rest = String(total % 60).padStart(2, "0");
    return `${minutes}:${rest}`;
  }

  function escapeHTML(value) {
    return String(value)
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;")
      .replaceAll("'", "&#039;");
  }

  soloButton.addEventListener("click", () => connect("solo"));
  createButton.addEventListener("click", () => connect("create"));
  joinForm.addEventListener("submit", (event) => {
    event.preventDefault();
    const code = roomCodeInput.value.trim().toUpperCase();
    if (code.length !== 6) {
      lobbyStatus.textContent = "Kode room harus terdiri dari 6 karakter.";
      return;
    }
    connect("join", code);
  });
  roomCodeInput.addEventListener("input", () => {
    roomCodeInput.value = roomCodeInput.value.toUpperCase().replace(/[^A-Z0-9]/g, "").slice(0, 6);
  });
  roomCodeButton.addEventListener("click", async () => {
    if (state.mode !== "pvp" || !state.code) return;
    try {
      await navigator.clipboard.writeText(state.code);
      setBattleStatus("Kode room disalin.");
    } catch {
      setBattleStatus(`Kode room: ${state.code}`);
    }
  });
  backToLobby.addEventListener("click", returnToLobby);
  window.addEventListener("resize", resizeCanvas);
  window.addEventListener("keydown", (event) => {
    if (event.repeat || battle.classList.contains("hidden")) return;
    const index = Number(event.key) - 1;
    if (index >= 0 && index < unitOrder.length) spawn(unitOrder[index]);
  });

  requestAnimationFrame(renderFrame);
})();
