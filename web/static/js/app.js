/**
 * Compliance Workspace v1.0 — Main Application Logic
 * 100% Vanilla JS, Offline-first, Zero External Dependencies.
 */

// State Aplikasi
const AppState = {
  currentView: "dashboard",
  workItems: [],
  selectedWorkItem: null,
  activeLogs: [],
  filters: {
    search: "",
    status: "",
    priority: "",
    category: "",
    include_archived: false,
    sort_by: "received_on",
    sort_order: "DESC"
  },
  isLoading: false
};

// Utilitas Format Tanggal & Tampilan
const UI = {
  // Format Tanggal YYYY-MM-DD ke Bahasa Indonesia (contoh: 17 Okt 2026)
  formatDateID(dateStr) {
    if (!dateStr) return "-";
    const parts = dateStr.split("-");
    if (parts.length !== 3) return dateStr;
    const year = parseInt(parts[0], 10);
    const month = parseInt(parts[1], 10) - 1;
    const day = parseInt(parts[2], 10);
    const months = ["Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agt", "Sep", "Okt", "Nov", "Des"];
    return `${day} ${months[month]} ${year}`;
  },

  // Format Waktu RFC3339 ke Asia/Jakarta (WIB)
  formatDateTimeID(isoStr) {
    if (!isoStr) return "-";
    try {
      const d = new Date(isoStr);
      if (isNaN(d.getTime())) return isoStr;
      return new Intl.DateTimeFormat("id-ID", {
        timeZone: "Asia/Jakarta",
        day: "numeric",
        month: "short",
        year: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        hour12: false
      }).format(d) + " WIB";
    } catch {
      return isoStr;
    }
  },

  // Dapatkan tanggal hari ini dalam format YYYY-MM-DD lokal
  getTodayYYYYMMDD() {
    const now = new Date();
    const y = now.getFullYear();
    const m = String(now.getMonth() + 1).padStart(2, "0");
    const d = String(now.getDate()).padStart(2, "0");
    return `${y}-${m}-${d}`;
  },

  // Dapatkan tanggal N hari ke depan format YYYY-MM-DD
  getDateOffsetYYYYMMDD(offsetDays) {
    const now = new Date();
    now.setDate(now.getDate() + offsetDays);
    const y = now.getFullYear();
    const m = String(now.getMonth() + 1).padStart(2, "0");
    const d = String(now.getDate()).padStart(2, "0");
    return `${y}-${m}-${d}`;
  },

  // Label Status Bahasa Indonesia
  getStatusLabel(status) {
    const map = {
      not_started: "Belum Mulai",
      in_progress: "Dalam Proses",
      waiting: "Menunggu",
      completed: "Selesai",
      cancelled: "Dibatalkan"
    };
    return map[status] || status;
  },

  // Label Prioritas Bahasa Indonesia
  getPriorityLabel(priority) {
    const map = {
      low: "Rendah",
      normal: "Normal",
      high: "Tinggi",
      critical: "Kritis"
    };
    return map[priority] || priority;
  },

  // Label Tipe Entri Log
  getEntryTypeLabel(entryType) {
    const map = {
      created: "Pekerjaan Dibuat",
      activity: "Catatan Progres",
      edited: "Pembaruan Data",
      status_changed: "Perubahan Status",
      archived: "Diarsipkan",
      restored: "Dipulihkan"
    };
    return map[entryType] || entryType;
  },

  // Toast Notification
  showToast(message, type = "info") {
    const container = document.getElementById("toastContainer");
    if (!container) return;

    const toast = document.createElement("div");
    toast.className = `toast ${type}`;
    toast.setAttribute("role", "alert");
    toast.textContent = message;

    container.appendChild(toast);
    setTimeout(() => {
      toast.style.opacity = "0";
      toast.style.transition = "opacity 0.3s";
      setTimeout(() => toast.remove(), 300);
    }, 3500);
  },

  // Buka Modal
  openModal(modalId) {
    const modal = document.getElementById(modalId);
    if (modal) {
      modal.classList.add("open");
      const firstInput = modal.querySelector("input, select, textarea, button");
      if (firstInput) firstInput.focus();
    }
  },

  // Tutup Modal
  closeModal(modalId) {
    const modal = document.getElementById(modalId);
    if (modal) modal.classList.remove("open");
  }
};

// Inisialisasi Aplikasi
document.addEventListener("DOMContentLoaded", async () => {
  setupNavigation();
  setupFilterListeners();
  setupModalListeners();
  setupFormListeners();

  // Inisialisasi API & ambil data
  try {
    await API.init();
    await loadWorkItems();
    updateLiveClock();
    setInterval(updateLiveClock, 30000);
  } catch (err) {
    UI.showToast(`Gagal terhubung ke server: ${err.message}`, "error");
  }
});

// Update Jam & Tanggal Header
function updateLiveClock() {
  const clockEl = document.getElementById("headerLiveClock");
  if (!clockEl) return;
  const now = new Date();
  const dateStr = new Intl.DateTimeFormat("id-ID", {
    timeZone: "Asia/Jakarta",
    weekday: "long",
    day: "numeric",
    month: "long",
    year: "numeric"
  }).format(now);
  clockEl.textContent = `${dateStr} (WIB)`;
}

// Setup Navigasi Sidebar
function setupNavigation() {
  const navButtons = document.querySelectorAll("[data-nav-view]");
  navButtons.forEach(btn => {
    btn.addEventListener("click", () => {
      const view = btn.getAttribute("data-nav-view");
      switchView(view);
    });
  });

  const refreshBtn = document.getElementById("btnRefreshData");
  if (refreshBtn) {
    refreshBtn.addEventListener("click", () => {
      loadWorkItems(true);
    });
  }
}

function switchView(viewName) {
  AppState.currentView = viewName;

  // Update Navigasi Aktif
  document.querySelectorAll("[data-nav-view]").forEach(btn => {
    if (btn.getAttribute("data-nav-view") === viewName) {
      btn.classList.add("active");
    } else {
      btn.classList.remove("active");
    }
  });

  // Ganti Bagian Tampilan
  document.querySelectorAll(".view-section").forEach(sec => {
    sec.classList.remove("active");
  });
  const targetSection = document.getElementById(`view-${viewName}`);
  if (targetSection) targetSection.classList.add("active");

  // Update Judul Halaman
  const titleMap = {
    dashboard: "Dashboard Kepatuhan",
    work_register: "Work Register",
    activity_log: "Activity Log Per Pekerjaan",
    deadline_tracker: "Deadline Tracker",
    settings_backup: "Pengaturan & Backup"
  };
  const titleEl = document.getElementById("pageTitle");
  if (titleEl) titleEl.textContent = titleMap[viewName] || "Compliance Workspace";

  // Render Ulang Tampilan Terkait
  if (viewName === "dashboard") renderDashboard();
  if (viewName === "work_register") renderWorkRegister();
  if (viewName === "activity_log") renderActivityLogView();
  if (viewName === "deadline_tracker") renderDeadlineTracker();
  if (viewName === "settings_backup") renderSettingsBackup();
}

// Muat Data Pekerjaan dari Server
async function loadWorkItems(notify = false) {
  AppState.isLoading = true;
  try {
    const items = await API.listWorkItems({
      include_archived: AppState.filters.include_archived,
      status: AppState.filters.status,
      priority: AppState.filters.priority,
      category: AppState.filters.category,
      search: AppState.filters.search,
      sort_by: AppState.filters.sort_by,
      sort_order: AppState.filters.sort_order
    });

    AppState.workItems = Array.isArray(items) ? items : [];

    // Perbarui Tampilan
    renderDashboard();
    renderWorkRegister();
    renderDeadlineTracker();
    populateCategoryFilter();
    populateActivityWorkSelect();

    if (notify) UI.showToast("Data berhasil diperbarui dari server", "success");
  } catch (err) {
    UI.showToast(`Gagal memuat pekerjaan: ${err.message}`, "error");
  } finally {
    AppState.isLoading = false;
  }
}

// Setup Event Filter & Pencarian
function setupFilterListeners() {
  const searchInput = document.getElementById("filterSearch");
  if (searchInput) {
    let debounceTimer = null;
    searchInput.addEventListener("input", (e) => {
      clearTimeout(debounceTimer);
      debounceTimer = setTimeout(() => {
        AppState.filters.search = e.target.value.trim();
        loadWorkItems();
      }, 300);
    });
  }

  const statusSelect = document.getElementById("filterStatus");
  if (statusSelect) {
    statusSelect.addEventListener("change", (e) => {
      AppState.filters.status = e.target.value;
      loadWorkItems();
    });
  }

  const prioritySelect = document.getElementById("filterPriority");
  if (prioritySelect) {
    prioritySelect.addEventListener("change", (e) => {
      AppState.filters.priority = e.target.value;
      loadWorkItems();
    });
  }

  const categorySelect = document.getElementById("filterCategory");
  if (categorySelect) {
    categorySelect.addEventListener("change", (e) => {
      AppState.filters.category = e.target.value;
      loadWorkItems();
    });
  }

  const archivedCheckbox = document.getElementById("filterIncludeArchived");
  if (archivedCheckbox) {
    archivedCheckbox.addEventListener("change", (e) => {
      AppState.filters.include_archived = e.target.checked;
      loadWorkItems();
    });
  }

  const sortSelect = document.getElementById("filterSort");
  if (sortSelect) {
    sortSelect.addEventListener("change", (e) => {
      const val = e.target.value;
      if (val === "due_on_asc") {
        AppState.filters.sort_by = "due_on";
        AppState.filters.sort_order = "ASC";
      } else if (val === "received_on_desc") {
        AppState.filters.sort_by = "received_on";
        AppState.filters.sort_order = "DESC";
      } else if (val === "priority") {
        AppState.filters.sort_by = "priority";
        AppState.filters.sort_order = "DESC";
      } else {
        AppState.filters.sort_by = "received_on";
        AppState.filters.sort_order = "DESC";
      }
      loadWorkItems();
    });
  }
}

function populateCategoryFilter() {
  const categorySelect = document.getElementById("filterCategory");
  if (!categorySelect) return;
  const currentVal = categorySelect.value;
  const categories = new Set();
  AppState.workItems.forEach(item => {
    if (item.category) categories.add(item.category);
  });

  // Kosongkan opsi kecuali "Semua Kategori"
  categorySelect.innerHTML = '<option value="">Semua Kategori</option>';
  Array.from(categories).sort().forEach(cat => {
    const opt = document.createElement("option");
    opt.value = cat;
    opt.textContent = cat;
    if (cat === currentVal) opt.selected = true;
    categorySelect.appendChild(opt);
  });
}

// -------------------------------------------------------------
// 1. Modul Dashboard
// -------------------------------------------------------------
function renderDashboard() {
  const today = UI.getTodayYYYYMMDD();
  const in7Days = UI.getDateOffsetYYYYMMDD(7);

  // Ambil hanya pekerjaan non-arsip untuk metrik operasional
  const nonArchived = AppState.workItems.filter(item => !item.archived_at);

  const total = nonArchived.length;
  const notStarted = nonArchived.filter(i => i.status === "not_started").length;
  const inProgress = nonArchived.filter(i => i.status === "in_progress").length;
  const waiting = nonArchived.filter(i => i.status === "waiting").length;
  const completed = nonArchived.filter(i => i.status === "completed").length;

  // Overdue: due_on < today dan bukan completed/cancelled
  const overdue = nonArchived.filter(i => {
    if (i.status === "completed" || i.status === "cancelled") return false;
    return i.due_on && i.due_on < today;
  }).length;

  // Due Soon: today <= due_on <= 7 hari dan bukan completed/cancelled
  const dueSoon = nonArchived.filter(i => {
    if (i.status === "completed" || i.status === "cancelled") return false;
    return i.due_on && i.due_on >= today && i.due_on <= in7Days;
  }).length;

  // Pasang angka ke kartu metrik
  document.getElementById("metricTotal").textContent = total;
  document.getElementById("metricActive").textContent = notStarted + inProgress + waiting;
  document.getElementById("metricCompleted").textContent = completed;
  document.getElementById("metricDueSoon").textContent = dueSoon;
  document.getElementById("metricOverdue").textContent = overdue;

  // Render Tabel Pekerjaan Mendesak di Dashboard
  const urgentTableBody = document.getElementById("dashboardUrgentTableBody");
  if (!urgentTableBody) return;
  urgentTableBody.innerHTML = "";

  const urgentItems = nonArchived.filter(i => {
    if (i.status === "completed" || i.status === "cancelled") return false;
    return i.due_on && i.due_on <= in7Days;
  }).sort((a, b) => (a.due_on || "").localeCompare(b.due_on || ""));

  if (urgentItems.length === 0) {
    urgentTableBody.innerHTML = `
      <tr>
        <td colspan="6" class="empty-state" style="padding: 24px;">
          Tidak ada pekerjaan mendesak atau melewati deadline saat ini.
        </td>
      </tr>
    `;
    return;
  }

  urgentItems.slice(0, 5).forEach(item => {
    const tr = document.createElement("tr");

    // Work Code
    const tdCode = document.createElement("td");
    const codeSpan = document.createElement("span");
    codeSpan.className = "work-code-badge";
    codeSpan.textContent = item.work_code;
    tdCode.appendChild(codeSpan);

    // Title
    const tdTitle = document.createElement("td");
    tdTitle.textContent = item.title;

    // Priority
    const tdPriority = document.createElement("td");
    tdPriority.appendChild(createPriorityBadge(item.priority));

    // Status
    const tdStatus = document.createElement("td");
    tdStatus.appendChild(createStatusBadge(item.status));

    // Due Date
    const tdDue = document.createElement("td");
    if (item.due_on && item.due_on < today) {
      tdDue.className = "text-overdue";
      tdDue.textContent = `${UI.formatDateID(item.due_on)} (Lewat)`;
    } else if (item.due_on === today) {
      tdDue.className = "text-due-today";
      tdDue.textContent = "Hari Ini";
    } else {
      tdDue.textContent = UI.formatDateID(item.due_on);
    }

    // Actions
    const tdAction = document.createElement("td");
    const btnDetail = document.createElement("button");
    btnDetail.className = "btn btn-secondary btn-sm";
    btnDetail.textContent = "Lihat";
    btnDetail.addEventListener("click", () => openWorkDetailModal(item.id));
    tdAction.appendChild(btnDetail);

    tr.append(tdCode, tdTitle, tdPriority, tdStatus, tdDue, tdAction);
    urgentTableBody.appendChild(tr);
  });
}

// -------------------------------------------------------------
// 2. Modul Work Register
// -------------------------------------------------------------
function renderWorkRegister() {
  const tbody = document.getElementById("workRegisterTableBody");
  if (!tbody) return;
  tbody.innerHTML = "";

  if (AppState.workItems.length === 0) {
    tbody.innerHTML = `
      <tr>
        <td colspan="8" class="empty-state">
          <div class="empty-state-title">Belum ada data pekerjaan</div>
          <div class="empty-state-desc">Gunakan tombol "Tambah Pekerjaan" untuk mencatat tugas kepatuhan baru.</div>
        </td>
      </tr>
    `;
    return;
  }

  const today = UI.getTodayYYYYMMDD();

  AppState.workItems.forEach(item => {
    const tr = document.createElement("tr");

    // Kode
    const tdCode = document.createElement("td");
    const codeSpan = document.createElement("span");
    codeSpan.className = "work-code-badge";
    codeSpan.textContent = item.work_code;
    tdCode.appendChild(codeSpan);

    // Judul & Kategori
    const tdTitle = document.createElement("td");
    const titleDiv = document.createElement("div");
    titleDiv.style.fontWeight = "600";
    titleDiv.textContent = item.title;
    const catDiv = document.createElement("div");
    catDiv.style.fontSize = "12px";
    catDiv.style.color = "var(--text-muted)";
    catDiv.textContent = item.category || "General";
    tdTitle.append(titleDiv, catDiv);

    // Pemberi Tugas
    const tdReq = document.createElement("td");
    tdReq.textContent = item.requested_by || "-";

    // Tanggal Diterima
    const tdReceived = document.createElement("td");
    tdReceived.textContent = UI.formatDateID(item.received_on);

    // Tenggat
    const tdDue = document.createElement("td");
    if (!item.due_on) {
      tdDue.textContent = "-";
    } else {
      if (item.status !== "completed" && item.status !== "cancelled" && item.due_on < today) {
        tdDue.className = "text-overdue";
        tdDue.textContent = `${UI.formatDateID(item.due_on)} (Lewat)`;
      } else {
        tdDue.textContent = UI.formatDateID(item.due_on);
      }
    }

    // Prioritas
    const tdPriority = document.createElement("td");
    tdPriority.appendChild(createPriorityBadge(item.priority));

    // Status
    const tdStatus = document.createElement("td");
    tdStatus.appendChild(createStatusBadge(item.status));

    // Aksi
    const tdAction = document.createElement("td");
    tdAction.style.whiteSpace = "nowrap";

    const btnGroup = document.createElement("div");
    btnGroup.style.display = "flex";
    btnGroup.style.gap = "6px";

    // Tombol Detail
    const btnDetail = document.createElement("button");
    btnDetail.className = "btn btn-secondary btn-sm";
    btnDetail.textContent = "Detail";
    btnDetail.addEventListener("click", () => openWorkDetailModal(item.id));

    // Tombol Edit
    const btnEdit = document.createElement("button");
    btnEdit.className = "btn btn-secondary btn-sm";
    btnEdit.textContent = "Edit";
    btnEdit.disabled = !!item.archived_at;
    btnEdit.addEventListener("click", () => openEditWorkModal(item));

    // Tombol Arsip / Pulihkan
    const btnArchive = document.createElement("button");
    btnArchive.className = "btn btn-secondary btn-sm";
    if (item.archived_at) {
      btnArchive.textContent = "Pulihkan";
      btnArchive.addEventListener("click", () => confirmRestoreWork(item));
    } else {
      btnArchive.textContent = "Arsip";
      btnArchive.addEventListener("click", () => confirmArchiveWork(item));
    }

    btnGroup.append(btnDetail, btnEdit, btnArchive);
    tdAction.appendChild(btnGroup);

    tr.append(tdCode, tdTitle, tdReq, tdReceived, tdDue, tdPriority, tdStatus, tdAction);
    tbody.appendChild(tr);
  });
}

function createStatusBadge(status) {
  const span = document.createElement("span");
  span.className = `badge badge-status-${status}`;
  const dot = document.createElement("span");
  dot.className = "badge-dot";
  span.appendChild(dot);
  span.appendChild(document.createTextNode(UI.getStatusLabel(status)));
  return span;
}

function createPriorityBadge(priority) {
  const span = document.createElement("span");
  span.className = `badge badge-priority-${priority}`;
  span.textContent = UI.getPriorityLabel(priority);
  return span;
}

// -------------------------------------------------------------
// 3. Modul Activity Log
// -------------------------------------------------------------
function renderActivityLogView() {
  populateActivityWorkSelect();
  const select = document.getElementById("activityWorkSelect");
  if (select && select.value) {
    loadWorkItemActivities(select.value, "activityViewTimeline");
  }
}

function populateActivityWorkSelect() {
  const select = document.getElementById("activityWorkSelect");
  if (!select) return;
  const currentVal = select.value;
  select.innerHTML = '<option value="">-- Pilih Pekerjaan Kepatuhan --</option>';

  AppState.workItems.forEach(item => {
    const opt = document.createElement("option");
    opt.value = item.id;
    opt.textContent = `[${item.work_code}] ${item.title}`;
    if (String(item.id) === String(currentVal)) opt.selected = true;
    select.appendChild(opt);
  });
}

async function loadWorkItemActivities(workItemId, containerId) {
  const container = document.getElementById(containerId);
  if (!container) return;
  container.innerHTML = '<div class="loading-wrap"><div class="spinner"></div><div>Memuat log aktivitas...</div></div>';

  try {
    const logs = await API.listActivities(workItemId);
    renderTimeline(container, logs);
  } catch (err) {
    container.innerHTML = `<div class="empty-state" style="color: var(--danger);">${err.message}</div>`;
  }
}

function renderTimeline(container, logs) {
  container.innerHTML = "";
  if (!logs || logs.length === 0) {
    container.innerHTML = '<div class="empty-state">Belum ada catatan aktivitas untuk pekerjaan ini.</div>';
    return;
  }

  const timeline = document.createElement("div");
  timeline.className = "timeline";

  logs.forEach(log => {
    const item = document.createElement("div");
    item.className = "timeline-item";

    const marker = document.createElement("div");
    marker.className = `timeline-marker ${log.entry_type}`;
    item.appendChild(marker);

    const content = document.createElement("div");
    content.className = "timeline-content";

    const header = document.createElement("div");
    header.className = "timeline-header";

    const badge = document.createElement("span");
    badge.className = "timeline-type-badge";
    badge.textContent = UI.getEntryTypeLabel(log.entry_type);

    const timeSpan = document.createElement("span");
    timeSpan.className = "timeline-time";
    timeSpan.textContent = UI.formatDateTimeID(log.occurred_at);

    header.append(badge, timeSpan);

    const desc = document.createElement("div");
    desc.className = "timeline-desc";
    desc.textContent = log.description;

    content.append(header, desc);

    // Jika ada diff nilai lama vs nilai baru
    if (log.old_value !== null && log.old_value !== undefined && log.new_value !== null && log.new_value !== undefined) {
      const diff = document.createElement("div");
      diff.className = "timeline-diff";

      const oldSpan = document.createElement("span");
      oldSpan.className = "diff-old";
      oldSpan.textContent = log.old_value || "(kosong)";

      const arrow = document.createElement("span");
      arrow.className = "diff-arrow";
      arrow.textContent = "➔";

      const newSpan = document.createElement("span");
      newSpan.className = "diff-new";
      newSpan.textContent = log.new_value || "(kosong)";

      diff.append(oldSpan, arrow, newSpan);
      content.appendChild(diff);
    }

    item.appendChild(content);
    timeline.appendChild(item);
  });

  container.appendChild(timeline);
}

// -------------------------------------------------------------
// 4. Modul Deadline Tracker
// -------------------------------------------------------------
function renderDeadlineTracker() {
  const colOverdue = document.getElementById("colOverdueItems");
  const colToday = document.getElementById("colTodayItems");
  const colUpcoming = document.getElementById("colUpcomingItems");
  const colNone = document.getElementById("colNoneItems");

  if (!colOverdue || !colToday || !colUpcoming || !colNone) return;

  colOverdue.innerHTML = "";
  colToday.innerHTML = "";
  colUpcoming.innerHTML = "";
  colNone.innerHTML = "";

  const today = UI.getTodayYYYYMMDD();
  const in7Days = UI.getDateOffsetYYYYMMDD(7);

  // Filter pekerjaan aktif (tidak diarsipkan, belum selesai/dibatalkan)
  const activeItems = AppState.workItems.filter(i => {
    return !i.archived_at && i.status !== "completed" && i.status !== "cancelled";
  });

  let countOverdue = 0;
  let countToday = 0;
  let countUpcoming = 0;
  let countNone = 0;

  activeItems.forEach(item => {
    if (!item.due_on) {
      countNone++;
      colNone.appendChild(createDeadlineItemCard(item));
    } else if (item.due_on < today) {
      countOverdue++;
      colOverdue.appendChild(createDeadlineItemCard(item, "overdue"));
    } else if (item.due_on === today) {
      countToday++;
      colToday.appendChild(createDeadlineItemCard(item, "today"));
    } else if (item.due_on <= in7Days) {
      countUpcoming++;
      colUpcoming.appendChild(createDeadlineItemCard(item, "upcoming"));
    } else {
      // Lebih dari 7 hari, tetap tampilkan di kolom upcoming dengan keterangan tanggal
      countUpcoming++;
      colUpcoming.appendChild(createDeadlineItemCard(item));
    }
  });

  document.getElementById("countOverdue").textContent = countOverdue;
  document.getElementById("countToday").textContent = countToday;
  document.getElementById("countUpcoming").textContent = countUpcoming;
  document.getElementById("countNone").textContent = countNone;

  if (countOverdue === 0) colOverdue.innerHTML = '<div class="empty-state" style="padding: 20px;">Tidak ada pekerjaan terlambat</div>';
  if (countToday === 0) colToday.innerHTML = '<div class="empty-state" style="padding: 20px;">Tidak ada tenggat hari ini</div>';
  if (countUpcoming === 0) colUpcoming.innerHTML = '<div class="empty-state" style="padding: 20px;">Tidak ada tenggat dalam 7 hari</div>';
  if (countNone === 0) colNone.innerHTML = '<div class="empty-state" style="padding: 20px;">Semua pekerjaan memiliki tenggat</div>';
}

function createDeadlineItemCard(item, urgency) {
  const card = document.createElement("div");
  card.className = "deadline-item-card";

  const top = document.createElement("div");
  top.style.display = "flex";
  top.style.alignItems = "center";
  top.style.justifyContent = "space-between";

  const code = document.createElement("span");
  code.className = "work-code-badge";
  code.textContent = item.work_code;

  top.appendChild(code);
  top.appendChild(createPriorityBadge(item.priority));

  const title = document.createElement("div");
  title.className = "deadline-item-title";
  title.textContent = item.title;

  const meta = document.createElement("div");
  meta.className = "deadline-item-meta";

  const statusSpan = document.createElement("span");
  statusSpan.textContent = UI.getStatusLabel(item.status);

  const dueSpan = document.createElement("span");
  if (urgency === "overdue") {
    dueSpan.className = "text-overdue";
    dueSpan.textContent = `Lewat: ${UI.formatDateID(item.due_on)}`;
  } else if (urgency === "today") {
    dueSpan.className = "text-due-today";
    dueSpan.textContent = "Hari Ini";
  } else if (item.due_on) {
    dueSpan.textContent = UI.formatDateID(item.due_on);
  } else {
    dueSpan.textContent = "Tanpa Tenggat";
  }

  meta.append(statusSpan, dueSpan);

  const btnAction = document.createElement("button");
  btnAction.className = "btn btn-secondary btn-sm";
  btnAction.style.width = "100%";
  btnAction.style.marginTop = "4px";
  btnAction.textContent = "Lihat & Tindak Lanjut";
  btnAction.addEventListener("click", () => openWorkDetailModal(item.id));

  card.append(top, title, meta, btnAction);
  return card;
}

// -------------------------------------------------------------
// Modals & Formulir
// -------------------------------------------------------------
function setupModalListeners() {
  document.querySelectorAll("[data-close-modal]").forEach(btn => {
    btn.addEventListener("click", () => {
      const modalId = btn.getAttribute("data-close-modal");
      UI.closeModal(modalId);
    });
  });

  const btnOpenCreate = document.getElementById("btnOpenCreateModal");
  if (btnOpenCreate) {
    btnOpenCreate.addEventListener("click", () => {
      resetCreateForm();
      UI.openModal("modalCreateWork");
    });
  }

  const actSelect = document.getElementById("activityWorkSelect");
  if (actSelect) {
    actSelect.addEventListener("change", (e) => {
      if (e.target.value) {
        loadWorkItemActivities(e.target.value, "activityViewTimeline");
      } else {
        document.getElementById("activityViewTimeline").innerHTML = '<div class="empty-state">Pilih pekerjaan untuk melihat log.</div>';
      }
    });
  }

  const btnCreateBackup = document.getElementById("btnCreateBackup");
  if (btnCreateBackup) {
    btnCreateBackup.addEventListener("click", handleCreateBackupClick);
  }
}

function setupFormListeners() {
  // Submit Form Tambah Pekerjaan
  const formCreate = document.getElementById("formCreateWork");
  if (formCreate) {
    formCreate.addEventListener("submit", async (e) => {
      e.preventDefault();
      const title = document.getElementById("createTitle").value.trim();
      const description = document.getElementById("createDescription").value.trim();
      const category = document.getElementById("createCategory").value.trim() || "General";
      const requestedBy = document.getElementById("createRequestedBy").value.trim();
      const receivedOn = document.getElementById("createReceivedOn").value;
      const dueOn = document.getElementById("createDueOn").value || null;
      const priority = document.getElementById("createPriority").value;

      if (!title) {
        UI.showToast("Judul pekerjaan wajib diisi", "error");
        return;
      }
      if (!receivedOn) {
        UI.showToast("Tanggal diterima wajib diisi", "error");
        return;
      }

      const submitBtn = formCreate.querySelector("button[type=submit]");
      submitBtn.disabled = true;

      try {
        const newItem = await API.createWorkItem({
          title,
          description,
          category,
          requested_by: requestedBy,
          received_on: receivedOn,
          due_on: dueOn,
          priority
        });

        UI.showToast(`Pekerjaan ${newItem.work_code} berhasil dibuat`, "success");
        UI.closeModal("modalCreateWork");
        await loadWorkItems();
      } catch (err) {
        UI.showToast(`Gagal membuat pekerjaan: ${err.message}`, "error");
      } finally {
        submitBtn.disabled = false;
      }
    });
  }

  // Submit Form Edit Pekerjaan
  const formEdit = document.getElementById("formEditWork");
  if (formEdit) {
    formEdit.addEventListener("submit", async (e) => {
      e.preventDefault();
      const id = document.getElementById("editWorkId").value;
      const title = document.getElementById("editTitle").value.trim();
      const description = document.getElementById("editDescription").value.trim();
      const category = document.getElementById("editCategory").value.trim() || "General";
      const requestedBy = document.getElementById("editRequestedBy").value.trim();
      const receivedOn = document.getElementById("editReceivedOn").value;
      const dueOn = document.getElementById("editDueOn").value || null;
      const priority = document.getElementById("editPriority").value;
      const status = document.getElementById("editStatus").value;

      if (!title || !receivedOn) {
        UI.showToast("Judul dan Tanggal Diterima wajib diisi", "error");
        return;
      }

      const submitBtn = formEdit.querySelector("button[type=submit]");
      submitBtn.disabled = true;

      try {
        const updated = await API.updateWorkItem(id, {
          title,
          description,
          category,
          requested_by: requestedBy,
          received_on: receivedOn,
          due_on: dueOn,
          priority,
          status
        });

        UI.showToast(`Pekerjaan ${updated.work_code} berhasil diperbarui`, "success");
        UI.closeModal("modalEditWork");
        await loadWorkItems();
      } catch (err) {
        UI.showToast(`Gagal memperbarui pekerjaan: ${err.message}`, "error");
      } finally {
        submitBtn.disabled = false;
      }
    });
  }

  // Submit Form Tambah Aktivitas Manual
  const formAddActivity = document.getElementById("formAddActivity");
  if (formAddActivity) {
    formAddActivity.addEventListener("submit", async (e) => {
      e.preventDefault();
      const workItemId = document.getElementById("activityModalWorkId").value;
      const desc = document.getElementById("activityDescription").value.trim();

      if (!desc) {
        UI.showToast("Deskripsi aktivitas wajib diisi", "error");
        return;
      }

      const submitBtn = formAddActivity.querySelector("button[type=submit]");
      submitBtn.disabled = true;

      try {
        await API.addActivity(workItemId, {
          description: desc
        });

        UI.showToast("Catatan aktivitas berhasil ditambahkan", "success");
        document.getElementById("activityDescription").value = "";

        // Render ulang timeline
        await loadWorkItemActivities(workItemId, "detailTimelineContainer");
        await loadWorkItems();
      } catch (err) {
        UI.showToast(`Gagal menambah aktivitas: ${err.message}`, "error");
      } finally {
        submitBtn.disabled = false;
      }
    });
  }
}

function resetCreateForm() {
  document.getElementById("createTitle").value = "";
  document.getElementById("createDescription").value = "";
  document.getElementById("createCategory").value = "General";
  document.getElementById("createRequestedBy").value = "";
  document.getElementById("createReceivedOn").value = UI.getTodayYYYYMMDD();
  document.getElementById("createDueOn").value = "";
  document.getElementById("createPriority").value = "normal";
}

function openEditWorkModal(item) {
  document.getElementById("editWorkId").value = item.id;
  document.getElementById("editWorkCodeDisplay").textContent = item.work_code;
  document.getElementById("editTitle").value = item.title;
  document.getElementById("editDescription").value = item.description || "";
  document.getElementById("editCategory").value = item.category || "General";
  document.getElementById("editRequestedBy").value = item.requested_by || "";
  document.getElementById("editReceivedOn").value = item.received_on;
  document.getElementById("editDueOn").value = item.due_on || "";
  document.getElementById("editPriority").value = item.priority;
  document.getElementById("editStatus").value = item.status;

  UI.openModal("modalEditWork");
}

async function openWorkDetailModal(workItemId) {
  try {
    const item = await API.getWorkItem(workItemId);
    AppState.selectedWorkItem = item;

    document.getElementById("detailWorkCode").textContent = item.work_code;
    document.getElementById("detailTitle").textContent = item.title;
    document.getElementById("detailCategory").textContent = item.category;
    document.getElementById("detailRequestedBy").textContent = item.requested_by || "-";
    document.getElementById("detailReceivedOn").textContent = UI.formatDateID(item.received_on);
    document.getElementById("detailDueOn").textContent = item.due_on ? UI.formatDateID(item.due_on) : "Tidak ada";

    const pBadge = document.getElementById("detailPriorityBadge");
    pBadge.innerHTML = "";
    pBadge.appendChild(createPriorityBadge(item.priority));

    const sBadge = document.getElementById("detailStatusBadge");
    sBadge.innerHTML = "";
    sBadge.appendChild(createStatusBadge(item.status));

    document.getElementById("detailDescription").textContent = item.description || "Tidak ada deskripsi rinci.";
    document.getElementById("activityModalWorkId").value = item.id;

    // Load Timeline
    await loadWorkItemActivities(item.id, "detailTimelineContainer");

    UI.openModal("modalWorkDetail");
  } catch (err) {
    UI.showToast(`Gagal memuat detail pekerjaan: ${err.message}`, "error");
  }
}

// Konfirmasi Pengarsipan
function confirmArchiveWork(item) {
  showConfirmDialog(
    "Konfirmasi Pengarsipan",
    `Apakah Anda yakin ingin mengarsipkan pekerjaan [${item.work_code}] "${item.title}"? Pekerjaan ini akan disembunyikan dari daftar aktif.`,
    async () => {
      try {
        await API.archiveWorkItem(item.id);
        UI.showToast(`Pekerjaan ${item.work_code} berhasil diarsipkan`, "success");
        await loadWorkItems();
      } catch (err) {
        UI.showToast(`Gagal mengarsipkan: ${err.message}`, "error");
      }
    }
  );
}

// Konfirmasi Pemulihan Arsip
function confirmRestoreWork(item) {
  showConfirmDialog(
    "Konfirmasi Pemulihan",
    `Pulihkan pekerjaan [${item.work_code}] "${item.title}" kembali ke daftar aktif?`,
    async () => {
      try {
        await API.restoreWorkItem(item.id);
        UI.showToast(`Pekerjaan ${item.work_code} berhasil dipulihkan`, "success");
        await loadWorkItems();
      } catch (err) {
        UI.showToast(`Gagal memulihkan: ${err.message}`, "error");
      }
    }
  );
}

// Dialog Konfirmasi Reusable
function showConfirmDialog(title, message, onConfirm) {
  document.getElementById("confirmDialogTitle").textContent = title;
  document.getElementById("confirmDialogMessage").textContent = message;

  const btnConfirm = document.getElementById("btnConfirmAction");
  // Bersihkan handler sebelumnya dengan kloning tombol
  const newBtn = btnConfirm.cloneNode(true);
  btnConfirm.parentNode.replaceChild(newBtn, btnConfirm);

  newBtn.addEventListener("click", async () => {
    UI.closeModal("modalConfirm");
    await onConfirm();
  });

  UI.openModal("modalConfirm");
}

// -------------------------------------------------------------
// 5. Modul Settings & Backup
// -------------------------------------------------------------
function renderSettingsBackup() {
  loadBackups();
}

async function loadBackups() {
  const tbody = document.getElementById("backupListTableBody");
  if (!tbody) return;
  tbody.innerHTML = '<tr><td colspan="5" class="empty-state"><div class="spinner"></div><div>Memuat daftar cadangan...</div></td></tr>';

  try {
    const backups = await API.listBackups();
    renderBackupTable(tbody, backups);
  } catch (err) {
    tbody.innerHTML = `<tr><td colspan="5" class="empty-state" style="color: var(--danger);">Gagal memuat cadangan: ${err.message}</td></tr>`;
  }
}

function renderBackupTable(tbody, backups) {
  tbody.innerHTML = "";
  if (!backups || backups.length === 0) {
    tbody.innerHTML = '<tr><td colspan="5" class="empty-state">Belum ada file cadangan. Klik "Buat Backup Sekarang" untuk membuat cadangan snapshot SQLite baru.</td></tr>';
    return;
  }

  backups.forEach(item => {
    const tr = document.createElement("tr");

    // Nama File
    const tdName = document.createElement("td");
    const nameSpan = document.createElement("span");
    nameSpan.className = "work-code-badge";
    nameSpan.textContent = item.filename;
    tdName.appendChild(nameSpan);

    // Tanggal Dibuat
    const tdDate = document.createElement("td");
    tdDate.textContent = UI.formatDateTimeID(item.created_at);

    // Ukuran File
    const tdSize = document.createElement("td");
    tdSize.textContent = item.formatted_size || `${item.size_bytes} B`;

    // Integritas
    const tdStatus = document.createElement("td");
    const statusBadge = document.createElement("span");
    statusBadge.className = "badge badge-status-completed";
    const dot = document.createElement("span");
    dot.className = "badge-dot";
    statusBadge.append(dot, document.createTextNode("Terverifikasi"));
    tdStatus.appendChild(statusBadge);

    // Aksi (Tombol Pulihkan)
    const tdAction = document.createElement("td");
    const btnRestore = document.createElement("button");
    btnRestore.className = "btn btn-secondary btn-sm";
    btnRestore.textContent = "Pulihkan Database";
    btnRestore.addEventListener("click", () => confirmRestoreBackup(item.filename));
    tdAction.appendChild(btnRestore);

    tr.append(tdName, tdDate, tdSize, tdStatus, tdAction);
    tbody.appendChild(tr);
  });
}

async function handleCreateBackupClick() {
  const btn = document.getElementById("btnCreateBackup");
  if (!btn) return;
  const originalText = btn.innerHTML;
  btn.disabled = true;
  btn.textContent = "Membuat Cadangan...";

  try {
    const res = await API.createBackup();
    UI.showToast(`Cadangan berhasil dibuat: ${res.filename} (${res.formatted_size})`, "success");
    await loadBackups();
  } catch (err) {
    UI.showToast(`Gagal membuat cadangan: ${err.message}`, "error");
  } finally {
    btn.disabled = false;
    btn.innerHTML = originalText;
  }
}

function confirmRestoreBackup(filename) {
  showConfirmDialog(
    "Konfirmasi Pemulihan Database",
    `PERINGATAN: Memulihkan database dari file cadangan "${filename}" akan menggantikan seluruh data database yang aktif saat ini. Salinan pengaman otomatis akan dibuat sebelum pemulihan. Apakah Anda yakin ingin memulihkan?`,
    async () => {
      try {
        const res = await API.restoreBackup(filename);
        UI.showToast(res.message || "Database berhasil dipulihkan", "success");
        await loadWorkItems();
        await loadBackups();
      } catch (err) {
        UI.showToast(`Gagal memulihkan database: ${err.message}`, "error");
      }
    }
  );
}
