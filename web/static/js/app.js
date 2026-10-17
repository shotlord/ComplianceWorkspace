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
  categories: [],
  allCategories: [],
  categoryModalCaller: null,
  templates: [],
  recurringSchedules: [],
  subtasks: [],
  activeTaskTab: "recurring",
  importState: {
    activeTab: "wizard",
    currentStep: 1,
    selectedFile: null,
    fileName: "",
    fileFormat: "",
    sheets: [],
    selectedSheet: "",
    headers: [],
    suggestedMapping: {},
    currentMapping: {},
    duplicateMode: "skip",
    previewResult: null
  },
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

  // Format Ukuran File (Bytes)
  formatBytes(bytes) {
    if (!bytes || bytes === 0) return "0 B";
    const k = 1024;
    const sizes = ["B", "KB", "MB", "GB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + " " + sizes[i];
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
  setupImportListeners();

  // Inisialisasi API & ambil data
  try {
    await API.init();
    await loadCategories();
    await loadTemplates();
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
    task_management: "Tugas, Template & Master Kategori",
    import_data: "Data Import Center (CSV & Excel XLSX)",
    settings_backup: "Pengaturan & Backup"
  };
  const titleEl = document.getElementById("pageTitle");
  if (titleEl) titleEl.textContent = titleMap[viewName] || "Compliance Workspace";

  // Render Ulang Tampilan Terkait
  if (viewName === "dashboard") renderDashboard();
  if (viewName === "work_register") renderWorkRegister();
  if (viewName === "activity_log") renderActivityLogView();
  if (viewName === "deadline_tracker") renderDeadlineTracker();
  if (viewName === "task_management") renderTaskManagementView();
  if (viewName === "import_data") renderImportDataView();
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

  if (Array.isArray(AppState.categories)) {
    AppState.categories.forEach(c => {
      if (c.name) categories.add(c.name);
    });
  }

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

async function loadCategories() {
  try {
    const activeCats = await API.listCategories(false);
    const allCats = await API.listCategories(true);
    AppState.categories = Array.isArray(activeCats) ? activeCats : [];
    AppState.allCategories = Array.isArray(allCats) ? allCats : [];
    populateCategoryFilter();
    populateCategorySelects();
    renderCategoryTable(AppState.allCategories);
    renderManageCategoryTable(AppState.allCategories);
  } catch (err) {
    console.error("Gagal memuat kategori:", err);
  }
}

function populateCategorySelects(preferredValue = null, targetSelectId = null) {
  const activeNames = [];
  if (Array.isArray(AppState.categories)) {
    AppState.categories.forEach(c => {
      if (c.name && !activeNames.includes(c.name)) activeNames.push(c.name);
    });
  }
  if (activeNames.length === 0) activeNames.push("General");

  const editingCategory = (AppState.selectedWorkItem && AppState.selectedWorkItem.category) ? AppState.selectedWorkItem.category : null;

  const targetIds = ["createCategory", "editCategory", "recCategory", "tplCategory"];
  targetIds.forEach(id => {
    const sel = document.getElementById(id);
    if (!sel) return;
    const currentVal = (targetSelectId === id && preferredValue) ? preferredValue : sel.value;

    const namesForThisSelect = [...activeNames];
    if (id === "editCategory" && editingCategory && !namesForThisSelect.includes(editingCategory)) {
      namesForThisSelect.push(editingCategory);
    }
    if (targetSelectId === id && preferredValue && !namesForThisSelect.includes(preferredValue)) {
      namesForThisSelect.push(preferredValue);
    }

    sel.innerHTML = "";
    namesForThisSelect.sort().forEach(cat => {
      const opt = document.createElement("option");
      opt.value = cat;
      opt.textContent = cat;
      if (cat === currentVal || (!currentVal && cat === "General")) opt.selected = true;
      sel.appendChild(opt);
    });

    if (targetSelectId === id && preferredValue) {
      sel.value = preferredValue;
    }
  });
}

async function loadTemplates() {
  try {
    const tpls = await API.listTemplates();
    AppState.templates = Array.isArray(tpls) ? tpls : [];
    populateTemplateSelects();
  } catch (err) {
    console.error("Gagal memuat template:", err);
  }
}

function populateTemplateSelects() {
  const selCreate = document.getElementById("createSelectTemplate");
  if (selCreate) {
    const curVal = selCreate.value;
    selCreate.innerHTML = '<option value="">-- Buat Manual (Tanpa Template) --</option>';
    AppState.templates.forEach(t => {
      const opt = document.createElement("option");
      opt.value = t.id;
      const count = Array.isArray(t.checklist) ? t.checklist.length : 0;
      opt.textContent = `${t.name} (${count} subtask)`;
      if (String(t.id) === String(curVal)) opt.selected = true;
      selCreate.appendChild(opt);
    });
  }

  const selRec = document.getElementById("recTemplateId");
  if (selRec) {
    const curVal = selRec.value;
    selRec.innerHTML = '<option value="">-- Tanpa Template (Kosong) --</option>';
    AppState.templates.forEach(t => {
      const opt = document.createElement("option");
      opt.value = t.id;
      const count = Array.isArray(t.checklist) ? t.checklist.length : 0;
      opt.textContent = `${t.name} (${count} subtask)`;
      if (String(t.id) === String(curVal)) opt.selected = true;
      selRec.appendChild(opt);
    });
  }
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

    // Progres (Sprint 2 - Bedakan Aktual vs Estimasi)
    const tdProgress = document.createElement("td");
    tdProgress.style.minWidth = "120px";
    const progPct = item.progress_percentage || 0;
    const progContainer = document.createElement("div");
    progContainer.className = "progress-container";
    const progBg = document.createElement("div");
    progBg.className = "progress-bar-bg";
    const progFill = document.createElement("div");
    progFill.className = `progress-bar-fill ${progPct === 100 ? "success" : ""}`;
    progFill.style.width = `${progPct}%`;
    progBg.appendChild(progFill);

    const progBadge = document.createElement("span");
    progBadge.className = "progress-badge";
    if (item.subtask_count > 0) {
      progBadge.title = "Progres aktual dihitung dari checklist subtask";
      progBadge.textContent = `Aktual: ${progPct}% (${item.completed_subtask_count}/${item.subtask_count})`;
    } else {
      progBadge.title = "Progres estimasi berdasarkan status pekerjaan";
      progBadge.textContent = `Estimasi: ${progPct}%`;
    }
    progContainer.append(progBg, progBadge);
    tdProgress.appendChild(progContainer);

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

    tr.append(tdCode, tdTitle, tdReq, tdReceived, tdDue, tdPriority, tdStatus, tdProgress, tdAction);
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
      populateCategorySelects();
      populateTemplateSelects();
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

  // Task Management Tab Switching
  document.querySelectorAll("[data-task-tab]").forEach(btn => {
    btn.addEventListener("click", () => {
      const tab = btn.getAttribute("data-task-tab");
      AppState.activeTaskTab = tab;

      document.querySelectorAll("[data-task-tab]").forEach(b => b.classList.remove("active"));
      btn.classList.add("active");

      const paneRec = document.getElementById("tabContentRecurring");
      const paneTpl = document.getElementById("tabContentTemplates");
      const paneCat = document.getElementById("tabContentCategories");
      if (paneRec) paneRec.style.display = tab === "recurring" ? "block" : "none";
      if (paneTpl) paneTpl.style.display = tab === "templates" ? "block" : "none";
      if (paneCat) paneCat.style.display = tab === "categories" ? "block" : "none";

      const labelEl = document.getElementById("btnNewTaskItemLabel");
      if (labelEl) {
        if (tab === "recurring") labelEl.textContent = "Tambah Jadwal";
        if (tab === "templates") labelEl.textContent = "Tambah Template";
        if (tab === "categories") labelEl.textContent = "Tambah Kategori";
      }
    });
  });

  const btnOpenNewTaskItem = document.getElementById("btnOpenNewTaskItemModal");
  if (btnOpenNewTaskItem) {
    btnOpenNewTaskItem.addEventListener("click", () => {
      if (AppState.activeTaskTab === "recurring") {
        openCreateRecurringModal();
      } else if (AppState.activeTaskTab === "templates") {
        openCreateTemplateModal();
      } else if (AppState.activeTaskTab === "categories") {
        UI.openModal("modalCreateCategory");
      }
    });
  }

  const btnTriggerRec = document.getElementById("btnTriggerRecurringManual");
  if (btnTriggerRec) {
    btnTriggerRec.addEventListener("click", async () => {
      btnTriggerRec.disabled = true;
      try {
        const res = await API.triggerRecurring();
        UI.showToast(`Pemeriksaan jadwal selesai. Dibuat: ${res.generated_count || 0} pekerjaan baru.`, "success");
        await loadRecurringSchedules();
        await loadWorkItems();
      } catch (err) {
        UI.showToast(`Gagal memeriksa jadwal: ${err.message}`, "error");
      } finally {
        btnTriggerRec.disabled = false;
      }
    });
  }

  // Listener Tombol Kelola Kategori dari Form Tambah / Edit Pekerjaan
  const btnManageCreate = document.getElementById("btnManageCategoriesFromCreate");
  if (btnManageCreate) {
    btnManageCreate.addEventListener("click", () => {
      openManageCategoriesModal("createCategory");
    });
  }

  const btnManageEdit = document.getElementById("btnManageCategoriesFromEdit");
  if (btnManageEdit) {
    btnManageEdit.addEventListener("click", () => {
      openManageCategoriesModal("editCategory");
    });
  }

  const btnToggleAdd = document.getElementById("btnToggleAddCategoryForm");
  if (btnToggleAdd) {
    btnToggleAdd.addEventListener("click", () => {
      const container = document.getElementById("categoryFormContainer");
      if (!container) return;
      if (container.style.display === "none" || !container.style.display) {
        resetCategoryForm();
        container.style.display = "block";
      } else {
        container.style.display = "none";
      }
    });
  }

  const btnCancelForm = document.getElementById("btnCancelCategoryForm");
  if (btnCancelForm) {
    btnCancelForm.addEventListener("click", () => {
      resetCategoryForm();
    });
  }
}

function setupFormListeners() {
  // Listener perubahan pilihan template di modal create work
  const selTpl = document.getElementById("createSelectTemplate");
  if (selTpl) {
    selTpl.addEventListener("change", (e) => {
      const tplId = e.target.value;
      if (!tplId) return;
      const tpl = AppState.templates.find(t => String(t.id) === String(tplId));
      if (tpl) {
        document.getElementById("createTitle").value = tpl.name;
        const catSel = document.getElementById("createCategory");
        if (catSel) catSel.value = tpl.category || "General";
        document.getElementById("createPriority").value = tpl.default_priority || "normal";
        document.getElementById("createDescription").value = tpl.description || "";
      }
    });
  }

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
      const tplId = document.getElementById("createSelectTemplate") ? document.getElementById("createSelectTemplate").value : "";

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

        // Terapkan template jika dipilih
        if (tplId) {
          try {
            await API.applyTemplate(newItem.id, tplId);
          } catch (tplErr) {
            console.error("Gagal menerapkan template checklist:", tplErr);
          }
        }

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

  // Submit Form Tambah Subtask Cepat di Modal Detail
  const formAddSubtask = document.getElementById("formAddSubtask");
  if (formAddSubtask) {
    formAddSubtask.addEventListener("submit", async (e) => {
      e.preventDefault();
      const input = document.getElementById("newSubtaskTitle");
      const title = input.value.trim();
      if (!title || !AppState.selectedWorkItem) return;

      const submitBtn = formAddSubtask.querySelector("button[type=submit]");
      submitBtn.disabled = true;

      try {
        await API.createSubtask(AppState.selectedWorkItem.id, { title });
        input.value = "";
        await loadWorkItemSubtasks(AppState.selectedWorkItem.id);
        await loadWorkItemActivities(AppState.selectedWorkItem.id, "detailTimelineContainer");
        await loadWorkItems();
      } catch (err) {
        UI.showToast(`Gagal menambahkan subtask: ${err.message}`, "error");
      } finally {
        submitBtn.disabled = false;
      }
    });
  }

  // Submit Form Tambah / Edit Jadwal Berulang
  const formRec = document.getElementById("formCreateRecurring");
  if (formRec) {
    formRec.addEventListener("submit", async (e) => {
      e.preventDefault();
      const idVal = document.getElementById("recId") ? document.getElementById("recId").value : "";
      const isEdit = !!idVal;
      const title = document.getElementById("recTitle").value.trim();
      const category = document.getElementById("recCategory").value.trim() || "General";
      const priority = document.getElementById("recPriority").value;
      const frequency = document.getElementById("recType").value;
      const intervalVal = parseInt(document.getElementById("recInterval").value, 10);
      const interval = isNaN(intervalVal) || intervalVal < 1 ? 1 : intervalVal;
      const startDate = document.getElementById("recStartDate").value;
      const endDate = document.getElementById("recEndDate").value || null;
      const templateIdVal = document.getElementById("recTemplateId").value;
      const templateId = templateIdVal ? parseInt(templateIdVal, 10) : null;
      const description = document.getElementById("recDescription").value.trim();

      if (!title) {
        UI.showToast("Judul Jadwal wajib diisi", "error");
        document.getElementById("recTitle").focus();
        return;
      }
      if (!startDate) {
        UI.showToast("Tanggal Mulai Berlaku wajib diisi", "error");
        document.getElementById("recStartDate").focus();
        return;
      }
      if (intervalVal < 1) {
        UI.showToast("Interval N harus berupa bilangan bulat positif (minimal 1)", "error");
        document.getElementById("recInterval").focus();
        return;
      }
      if (endDate && endDate < startDate) {
        UI.showToast("Tanggal Selesai tidak boleh mendahului Tanggal Mulai", "error");
        document.getElementById("recEndDate").focus();
        return;
      }

      const submitBtn = document.getElementById("btnSubmitRecurring") || formRec.querySelector("button[type=submit]");
      const originalText = submitBtn.textContent;
      submitBtn.disabled = true;
      submitBtn.textContent = "Menyimpan...";

      try {
        const payload = {
          title,
          category,
          priority,
          frequency,
          interval_value: interval,
          start_date: startDate,
          end_date: endDate,
          template_id: templateId,
          description
        };

        if (isEdit) {
          await API.updateRecurringSchedule(idVal, payload);
          UI.showToast("Jadwal berulang berhasil diperbarui", "success");
        } else {
          await API.createRecurringSchedule(payload);
          UI.showToast("Jadwal berulang berhasil ditambahkan", "success");
        }
        UI.closeModal("modalCreateRecurring");
        formRec.reset();
        if (document.getElementById("recId")) document.getElementById("recId").value = "";
        await loadRecurringSchedules();
      } catch (err) {
        UI.showToast(`Gagal menyimpan jadwal: ${err.message}`, "error");
      } finally {
        submitBtn.disabled = false;
        submitBtn.textContent = originalText;
      }
    });
  }

  // Submit Form Tambah / Edit Template
  const formTpl = document.getElementById("formCreateTemplate");
  if (formTpl) {
    formTpl.addEventListener("submit", async (e) => {
      e.preventDefault();
      const idVal = document.getElementById("tplId") ? document.getElementById("tplId").value : "";
      const isEdit = !!idVal;
      const name = document.getElementById("tplName").value.trim();
      const titlePattern = document.getElementById("tplTitlePattern") ? document.getElementById("tplTitlePattern").value.trim() : "";
      const category = document.getElementById("tplCategory").value.trim() || "General";
      const priority = document.getElementById("tplPriority").value;
      const description = document.getElementById("tplDescription").value.trim();
      const checklistRaw = document.getElementById("tplChecklist").value;

      if (!name) {
        UI.showToast("Nama template wajib diisi", "error");
        document.getElementById("tplName").focus();
        return;
      }
      if (!titlePattern) {
        UI.showToast("Pola Judul Pekerjaan (title_pattern) wajib diisi", "error");
        if (document.getElementById("tplTitlePattern")) document.getElementById("tplTitlePattern").focus();
        return;
      }

      const checklistItems = checklistRaw
        .split("\n")
        .map(line => line.trim())
        .filter(line => line.length > 0);

      const submitBtn = document.getElementById("btnSubmitTemplate") || formTpl.querySelector("button[type=submit]");
      const originalText = submitBtn.textContent;
      submitBtn.disabled = true;
      submitBtn.textContent = "Menyimpan...";

      try {
        const payload = {
          name,
          title_pattern: titlePattern,
          category,
          priority,
          description,
          checklist_items: checklistItems
        };

        if (isEdit) {
          await API.updateTemplate(idVal, payload);
          UI.showToast("Template pekerjaan berhasil diperbarui", "success");
        } else {
          await API.createTemplate(payload);
          UI.showToast("Template pekerjaan berhasil ditambahkan", "success");
        }
        UI.closeModal("modalCreateTemplate");
        formTpl.reset();
        if (document.getElementById("tplId")) document.getElementById("tplId").value = "";
        await loadTemplates();
      } catch (err) {
        UI.showToast(`Gagal menyimpan template: ${err.message}`, "error");
      } finally {
        submitBtn.disabled = false;
        submitBtn.textContent = originalText;
      }
    });
  }

  // Submit Form Tambah Kategori
  const formCat = document.getElementById("formCreateCategory");
  if (formCat) {
    formCat.addEventListener("submit", async (e) => {
      e.preventDefault();
      const name = document.getElementById("catName").value.trim();
      const color = document.getElementById("catColor").value;
      const description = document.getElementById("catDescription").value.trim();

      if (!name) {
        UI.showToast("Nama kategori wajib diisi", "error");
        return;
      }

      const submitBtn = formCat.querySelector("button[type=submit]");
      submitBtn.disabled = true;

      try {
        await API.createCategory({
          name,
          color,
          description
        });
        UI.showToast(`Kategori "${name}" berhasil ditambahkan`, "success");
        UI.closeModal("modalCreateCategory");
        formCat.reset();
        await loadCategories();
      } catch (err) {
        UI.showToast(`Gagal menambahkan kategori: ${err.message}`, "error");
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

  // Submit Form Kelola Kategori (Tambah / Edit Kategori Master)
  const formSaveCat = document.getElementById("formSaveCategory");
  if (formSaveCat) {
    formSaveCat.addEventListener("submit", async (e) => {
      e.preventDefault();
      const id = document.getElementById("manageCatId").value;
      const name = document.getElementById("manageCatName").value.trim();
      const color = document.getElementById("manageCatColor").value;
      const description = document.getElementById("manageCatDesc").value.trim();
      const isActive = document.getElementById("manageCatIsActive") ? document.getElementById("manageCatIsActive").checked : true;

      if (!name) {
        UI.showToast("Nama kategori wajib diisi", "error");
        return;
      }

      const submitBtn = document.getElementById("btnSaveCategorySubmit");
      if (submitBtn) submitBtn.disabled = true;

      try {
        if (id) {
          const updated = await API.updateCategory(id, {
            name,
            color,
            description,
            is_active: isActive
          });
          UI.showToast(`Kategori "${updated.name}" berhasil diperbarui`, "success");
          resetCategoryForm();
          await loadCategories();
          if (AppState.categoryModalCaller) {
            populateCategorySelects(updated.name, AppState.categoryModalCaller);
          }
        } else {
          const created = await API.createCategory({
            name,
            color,
            description
          });
          UI.showToast(`Kategori "${created.name}" berhasil ditambahkan`, "success");
          resetCategoryForm();
          await loadCategories();
          if (AppState.categoryModalCaller) {
            populateCategorySelects(created.name, AppState.categoryModalCaller);
          }
        }
      } catch (err) {
        UI.showToast(`Gagal menyimpan kategori: ${err.message}`, "error");
      } finally {
        if (submitBtn) submitBtn.disabled = false;
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
  AppState.selectedWorkItem = item;
  document.getElementById("editWorkId").value = item.id;
  document.getElementById("editWorkCodeDisplay").textContent = item.work_code;
  document.getElementById("editTitle").value = item.title;
  document.getElementById("editDescription").value = item.description || "";
  populateCategorySelects(item.category, "editCategory");
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

    // Load Timeline & Subtasks
    await loadWorkItemActivities(item.id, "detailTimelineContainer");
    await loadWorkItemSubtasks(item.id);

    UI.openModal("modalWorkDetail");
  } catch (err) {
    UI.showToast(`Gagal memuat detail pekerjaan: ${err.message}`, "error");
  }
}

async function loadWorkItemSubtasks(workItemId) {
  const container = document.getElementById("detailSubtaskList");
  const ratioEl = document.getElementById("detailSubtaskRatio");
  const fillEl = document.getElementById("detailSubtaskProgressFill");
  if (!container) return;

  container.innerHTML = '<div class="loading-wrap"><div class="spinner"></div><div>Memuat subtask...</div></div>';

  try {
    const subtasks = await API.listSubtasks(workItemId);
    AppState.subtasks = Array.isArray(subtasks) ? subtasks : [];

    const total = AppState.subtasks.length;
    const completed = AppState.subtasks.filter(s => s.is_completed).length;
    const pct = total === 0 ? (AppState.selectedWorkItem ? AppState.selectedWorkItem.progress_percentage : 0) : Math.round((completed / total) * 100);

    if (ratioEl) ratioEl.textContent = `${completed} / ${total} (${pct}%)`;
    if (fillEl) {
      fillEl.style.width = `${pct}%`;
      if (pct === 100) fillEl.classList.add("success");
      else fillEl.classList.remove("success");
    }

    renderSubtaskList(container, AppState.subtasks, workItemId);
  } catch (err) {
    container.innerHTML = `<div class="empty-state" style="color: var(--danger);">${err.message}</div>`;
  }
}

function renderSubtaskList(container, subtasks, workItemId) {
  container.innerHTML = "";
  if (!subtasks || subtasks.length === 0) {
    container.innerHTML = '<div class="empty-state" style="padding: 12px;">Belum ada subtask/checklist. Tambahkan checklist di bawah.</div>';
    return;
  }

  subtasks.forEach(s => {
    const item = document.createElement("div");
    item.className = `subtask-item ${s.is_completed ? "completed" : ""}`;

    const cb = document.createElement("input");
    cb.type = "checkbox";
    cb.className = "subtask-checkbox";
    cb.checked = !!s.is_completed;
    cb.addEventListener("change", async () => {
      try {
        await API.updateSubtask(s.id, { is_completed: cb.checked });
        await loadWorkItemSubtasks(workItemId);
        await loadWorkItemActivities(workItemId, "detailTimelineContainer");
        await loadWorkItems();
      } catch (err) {
        UI.showToast(`Gagal memperbarui status checklist: ${err.message}`, "error");
        cb.checked = !cb.checked;
      }
    });

    const titleSpan = document.createElement("span");
    titleSpan.className = "subtask-title";
    titleSpan.textContent = s.title;

    const btnDelete = document.createElement("button");
    btnDelete.className = "btn-delete-subtask";
    btnDelete.title = "Hapus subtask";
    btnDelete.innerHTML = "&times;";
    btnDelete.addEventListener("click", async () => {
      try {
        await API.deleteSubtask(s.id);
        UI.showToast("Subtask dihapus", "info");
        await loadWorkItemSubtasks(workItemId);
        await loadWorkItemActivities(workItemId, "detailTimelineContainer");
        await loadWorkItems();
      } catch (err) {
        UI.showToast(`Gagal menghapus subtask: ${err.message}`, "error");
      }
    });

    item.append(cb, titleSpan, btnDelete);
    container.appendChild(item);
  });
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

// -------------------------------------------------------------
// 6. Modul Task Management (Sprint 2)
// -------------------------------------------------------------
function renderTaskManagementView() {
  loadRecurringSchedules();
  loadTemplates();
  loadCategories();
}

async function loadRecurringSchedules() {
  const tbody = document.getElementById("recurringTableBody");
  if (tbody) tbody.innerHTML = '<tr><td colspan="8" class="empty-state"><div class="spinner"></div><div>Memuat jadwal berulang...</div></td></tr>';

  try {
    const schedules = await API.listRecurringSchedules();
    AppState.recurringSchedules = Array.isArray(schedules) ? schedules : [];
    renderRecurringTable(AppState.recurringSchedules);
  } catch (err) {
    if (tbody) tbody.innerHTML = `<tr><td colspan="8" class="empty-state" style="color: var(--danger);">Gagal memuat jadwal: ${err.message}</td></tr>`;
  }
}

function openCreateRecurringModal() {
  const form = document.getElementById("formCreateRecurring");
  if (form) form.reset();
  const idEl = document.getElementById("recId");
  if (idEl) idEl.value = "";
  const titleEl = document.getElementById("modalRecurringTitle");
  if (titleEl) titleEl.textContent = "Tambah Jadwal Pekerjaan Berulang (Recurring)";
  const submitBtn = document.getElementById("btnSubmitRecurring");
  if (submitBtn) submitBtn.textContent = "Simpan Jadwal";

  populateCategorySelects();
  populateTemplateSelects();
  const startInput = document.getElementById("recStartDate");
  if (startInput) startInput.value = UI.getTodayYYYYMMDD();
  const intervalInput = document.getElementById("recInterval");
  if (intervalInput) intervalInput.value = "1";
  const typeSelect = document.getElementById("recType");
  if (typeSelect) typeSelect.value = "monthly";

  UI.openModal("modalCreateRecurring");
}

function openEditRecurringModal(s) {
  const form = document.getElementById("formCreateRecurring");
  if (form) form.reset();
  const idEl = document.getElementById("recId");
  if (idEl) idEl.value = s.id;
  const titleEl = document.getElementById("modalRecurringTitle");
  if (titleEl) titleEl.textContent = "Edit Jadwal Pekerjaan Berulang";
  const submitBtn = document.getElementById("btnSubmitRecurring");
  if (submitBtn) submitBtn.textContent = "Simpan Perubahan";

  populateCategorySelects(s.category, "recCategory");
  populateTemplateSelects();

  if (document.getElementById("recTitle")) document.getElementById("recTitle").value = s.title || "";
  if (document.getElementById("recCategory")) document.getElementById("recCategory").value = s.category || "General";
  if (document.getElementById("recPriority")) document.getElementById("recPriority").value = s.priority || "normal";
  if (document.getElementById("recType")) document.getElementById("recType").value = s.frequency || "monthly";
  if (document.getElementById("recInterval")) document.getElementById("recInterval").value = s.interval_value || 1;
  if (document.getElementById("recStartDate")) document.getElementById("recStartDate").value = s.start_date || "";
  if (document.getElementById("recEndDate")) document.getElementById("recEndDate").value = s.end_date || "";
  if (document.getElementById("recTemplateId")) document.getElementById("recTemplateId").value = s.template_id || "";
  if (document.getElementById("recDescription")) document.getElementById("recDescription").value = s.description || "";

  UI.openModal("modalCreateRecurring");
}

function openCreateTemplateModal() {
  const form = document.getElementById("formCreateTemplate");
  if (form) form.reset();
  const idEl = document.getElementById("tplId");
  if (idEl) idEl.value = "";
  const titleEl = document.getElementById("modalTemplateTitle");
  if (titleEl) titleEl.textContent = "Tambah Template Pekerjaan Baru";
  const submitBtn = document.getElementById("btnSubmitTemplate");
  if (submitBtn) submitBtn.textContent = "Simpan Template";

  populateCategorySelects();
  UI.openModal("modalCreateTemplate");
}

function openEditTemplateModal(t) {
  const form = document.getElementById("formCreateTemplate");
  if (form) form.reset();
  const idEl = document.getElementById("tplId");
  if (idEl) idEl.value = t.id;
  const titleEl = document.getElementById("modalTemplateTitle");
  if (titleEl) titleEl.textContent = "Edit Template Pekerjaan";
  const submitBtn = document.getElementById("btnSubmitTemplate");
  if (submitBtn) submitBtn.textContent = "Simpan Perubahan";

  populateCategorySelects(t.category, "tplCategory");

  if (document.getElementById("tplName")) document.getElementById("tplName").value = t.name || "";
  if (document.getElementById("tplTitlePattern")) document.getElementById("tplTitlePattern").value = t.title_pattern || "";
  if (document.getElementById("tplCategory")) document.getElementById("tplCategory").value = t.category || "General";
  if (document.getElementById("tplPriority")) document.getElementById("tplPriority").value = t.priority || "normal";
  if (document.getElementById("tplDescription")) document.getElementById("tplDescription").value = t.description || "";
  if (document.getElementById("tplChecklist")) {
    const items = Array.isArray(t.checklist_items) ? t.checklist_items : [];
    document.getElementById("tplChecklist").value = items.join("\n");
  }

  UI.openModal("modalCreateTemplate");
}

function renderRecurringTable(schedules) {
  const tbody = document.getElementById("recurringTableBody");
  if (!tbody) return;
  tbody.innerHTML = "";

  if (!schedules || schedules.length === 0) {
    tbody.innerHTML = '<tr><td colspan="8" class="empty-state">Belum ada jadwal pekerjaan berulang. Klik "Tambah Jadwal" untuk membuat rutinitas baru.</td></tr>';
    return;
  }

  schedules.forEach(s => {
    const tr = document.createElement("tr");

    // Judul
    const tdTitle = document.createElement("td");
    const titleDiv = document.createElement("div");
    titleDiv.style.fontWeight = "600";
    titleDiv.textContent = s.title;
    tdTitle.appendChild(titleDiv);
    if (s.description) {
      const descDiv = document.createElement("div");
      descDiv.style.fontSize = "11.5px";
      descDiv.style.color = "var(--text-muted)";
      descDiv.textContent = s.description;
      tdTitle.appendChild(descDiv);
    }

    // Kategori
    const tdCat = document.createElement("td");
    tdCat.textContent = s.category;

    // Prioritas
    const tdPriority = document.createElement("td");
    tdPriority.appendChild(createPriorityBadge(s.priority));

    // Frekuensi
    const tdFreq = document.createElement("td");
    const freqLabels = {
      daily: "Harian",
      weekly: "Mingguan",
      monthly: "Bulanan",
      quarterly: "Triwulanan",
      yearly: "Tahunan"
    };
    const freqLabel = freqLabels[s.frequency] || s.frequency || "Bulanan";
    tdFreq.textContent = `${freqLabel} (Setiap ${s.interval_value || 1})`;

    // Jadwal Berikutnya
    const tdNext = document.createElement("td");
    tdNext.textContent = UI.formatDateID(s.next_run_date);

    // Terakhir Berjalan
    const tdLast = document.createElement("td");
    tdLast.textContent = s.last_generated_date ? UI.formatDateID(s.last_generated_date) : "Belum pernah";

    // Status (Termasuk evaluasi CASE 4: Perlu Tinjauan Kategori jika kategori nonaktif)
    const tdStatus = document.createElement("td");
    const catObj = (AppState.allCategories || []).find(c => c.name && c.name.toLowerCase() === (s.category || "").toLowerCase());
    const isCategoryInactive = catObj && !catObj.is_active;

    if (isCategoryInactive && s.is_active) {
      const badgeReview = document.createElement("span");
      badgeReview.className = "badge badge-status-waiting";
      badgeReview.title = "Kategori jadwal ini sedang dinonaktifkan. Generator ditahan sampai kategori diaktifkan kembali atau diganti.";
      const dot = document.createElement("span");
      dot.className = "badge-dot";
      badgeReview.append(dot, document.createTextNode("Perlu Tinjauan Kategori"));
      tdStatus.appendChild(badgeReview);
    } else {
      const badge = document.createElement("span");
      badge.className = `badge ${s.is_active ? "badge-status-completed" : "badge-status-cancelled"}`;
      const dot = document.createElement("span");
      dot.className = "badge-dot";
      badge.append(dot, document.createTextNode(s.is_active ? "Aktif" : "Nonaktif"));
      tdStatus.appendChild(badge);
    }

    // Aksi
    const tdAction = document.createElement("td");
    tdAction.style.whiteSpace = "nowrap";
    const btnGroup = document.createElement("div");
    btnGroup.style.display = "flex";
    btnGroup.style.gap = "6px";

    const btnEdit = document.createElement("button");
    btnEdit.className = "btn btn-secondary btn-sm";
    btnEdit.textContent = "Edit";
    btnEdit.addEventListener("click", () => openEditRecurringModal(s));

    const btnToggle = document.createElement("button");
    btnToggle.className = "btn btn-secondary btn-sm";
    btnToggle.textContent = s.is_active ? "Nonaktifkan" : "Aktifkan";
    btnToggle.addEventListener("click", async () => {
      try {
        await API.updateRecurringSchedule(s.id, { is_active: !s.is_active });
        UI.showToast(`Jadwal ${s.is_active ? "dinonaktifkan" : "diaktifkan"}`, "info");
        await loadRecurringSchedules();
      } catch (err) {
        UI.showToast(`Gagal mengubah status jadwal: ${err.message}`, "error");
      }
    });

    const btnDelete = document.createElement("button");
    btnDelete.className = "btn btn-secondary btn-sm";
    btnDelete.textContent = "Hapus";
    btnDelete.addEventListener("click", () => {
      showConfirmDialog("Hapus Jadwal Berulang", `Hapus jadwal "${s.title}"? Pekerjaan yang telah dibuat sebelumnya tidak akan terpengaruh.`, async () => {
        try {
          await API.deleteRecurringSchedule(s.id);
          UI.showToast("Jadwal berulang berhasil dihapus", "success");
          await loadRecurringSchedules();
        } catch (err) {
          UI.showToast(`Gagal menghapus jadwal: ${err.message}`, "error");
        }
      });
    });

    btnGroup.append(btnEdit, btnToggle, btnDelete);
    tdAction.appendChild(btnGroup);

    tr.append(tdTitle, tdCat, tdPriority, tdFreq, tdNext, tdLast, tdStatus, tdAction);
    tbody.appendChild(tr);
  });
}

function renderTemplateTable(templates) {
  const tbody = document.getElementById("templateTableBody");
  if (!tbody) return;
  tbody.innerHTML = "";

  if (!templates || templates.length === 0) {
    tbody.innerHTML = '<tr><td colspan="6" class="empty-state">Belum ada template pekerjaan. Klik "Tambah Template" untuk membuat template checklist.</td></tr>';
    return;
  }

  templates.forEach(t => {
    const tr = document.createElement("tr");

    // Nama & Pola Judul
    const tdName = document.createElement("td");
    const nameDiv = document.createElement("div");
    nameDiv.style.fontWeight = "600";
    nameDiv.textContent = t.name;
    tdName.appendChild(nameDiv);
    if (t.title_pattern) {
      const patternDiv = document.createElement("div");
      patternDiv.style.fontSize = "11.5px";
      patternDiv.style.color = "var(--text-muted)";
      patternDiv.textContent = `Pola: ${t.title_pattern}`;
      tdName.appendChild(patternDiv);
    }

    // Kategori
    const tdCat = document.createElement("td");
    tdCat.textContent = t.category;

    // Prioritas Default
    const tdPriority = document.createElement("td");
    tdPriority.appendChild(createPriorityBadge(t.priority));

    // Jumlah Checklist
    const tdChecklist = document.createElement("td");
    const count = Array.isArray(t.checklist_items) ? t.checklist_items.length : 0;
    tdChecklist.innerHTML = `<span class="progress-badge">${count} item subtask</span>`;

    // Deskripsi
    const tdDesc = document.createElement("td");
    tdDesc.textContent = t.description || "-";

    // Aksi
    const tdAction = document.createElement("td");
    tdAction.style.whiteSpace = "nowrap";
    const btnGroup = document.createElement("div");
    btnGroup.style.display = "flex";
    btnGroup.style.gap = "6px";

    const btnUse = document.createElement("button");
    btnUse.className = "btn btn-primary btn-sm";
    btnUse.textContent = "Gunakan";
    btnUse.addEventListener("click", () => {
      resetCreateForm();
      populateCategorySelects();
      populateTemplateSelects();
      const sel = document.getElementById("createSelectTemplate");
      if (sel) {
        sel.value = t.id;
        sel.dispatchEvent(new Event("change"));
      }
      UI.openModal("modalCreateWork");
    });

    const btnEdit = document.createElement("button");
    btnEdit.className = "btn btn-secondary btn-sm";
    btnEdit.textContent = "Edit";
    btnEdit.addEventListener("click", () => openEditTemplateModal(t));

    const btnDelete = document.createElement("button");
    btnDelete.className = "btn btn-secondary btn-sm";
    btnDelete.textContent = "Hapus";
    btnDelete.addEventListener("click", () => {
      showConfirmDialog("Hapus Template", `Apakah Anda yakin ingin menghapus template "${t.name}"?`, async () => {
        try {
          await API.deleteTemplate(t.id);
          UI.showToast("Template berhasil dihapus", "success");
          await loadTemplates();
        } catch (err) {
          UI.showToast(`Gagal menghapus template: ${err.message}`, "error");
        }
      });
    });

    btnGroup.append(btnUse, btnEdit, btnDelete);
    tdAction.appendChild(btnGroup);

    tr.append(tdName, tdCat, tdPriority, tdChecklist, tdDesc, tdAction);
    tbody.appendChild(tr);
  });
}

function renderCategoryTable(categories) {
  const tbody = document.getElementById("categoryTableBody");
  if (!tbody) return;
  tbody.innerHTML = "";

  if (!categories || categories.length === 0) {
    tbody.innerHTML = '<tr><td colspan="6" class="empty-state">Belum ada data kategori.</td></tr>';
    return;
  }

  categories.forEach(c => {
    const tr = document.createElement("tr");

    // Nama
    const tdName = document.createElement("td");
    tdName.style.fontWeight = "600";
    tdName.textContent = c.name;

    // Label & Warna
    const tdColor = document.createElement("td");
    const badge = document.createElement("span");
    badge.className = "category-badge-pill";
    badge.style.backgroundColor = c.color || "#0284c7";
    badge.textContent = c.name;
    tdColor.appendChild(badge);

    // Deskripsi
    const tdDesc = document.createElement("td");
    tdDesc.textContent = c.description || "-";

    // Tipe
    const tdType = document.createElement("td");
    if (c.is_default) {
      tdType.innerHTML = '<span class="badge badge-status-completed"><span class="badge-dot"></span>Default Sistem</span>';
    } else {
      tdType.innerHTML = '<span class="badge badge-status-not_started"><span class="badge-dot"></span>Kustom</span>';
    }

    // Status (Aktif / Nonaktif)
    const tdStatus = document.createElement("td");
    const badgeStatus = document.createElement("span");
    badgeStatus.className = `badge ${c.is_active ? "badge-status-completed" : "badge-status-cancelled"}`;
    const dot = document.createElement("span");
    dot.className = "badge-dot";
    badgeStatus.append(dot, document.createTextNode(c.is_active ? "Aktif" : "Nonaktif"));
    tdStatus.appendChild(badgeStatus);

    // Aksi
    const tdAction = document.createElement("td");
    tdAction.style.whiteSpace = "nowrap";

    if (!c.is_default) {
      const btnGroup = document.createElement("div");
      btnGroup.style.display = "flex";
      btnGroup.style.gap = "6px";

      const btnToggle = document.createElement("button");
      btnToggle.className = "btn btn-secondary btn-sm";
      btnToggle.textContent = c.is_active ? "Nonaktifkan" : "Aktifkan";
      btnToggle.addEventListener("click", async () => {
        try {
          await API.updateCategory(c.id, {
            name: c.name,
            color: c.color,
            description: c.description,
            is_active: !c.is_active
          });
          const newStatus = !c.is_active ? "diaktifkan" : "dinonaktifkan";
          UI.showToast(`Kategori "${c.name}" berhasil ${newStatus}`, "info");
          await loadCategories();
        } catch (err) {
          UI.showToast(`Gagal mengubah status kategori: ${err.message}`, "error");
        }
      });

      const btnDelete = document.createElement("button");
      btnDelete.className = "btn btn-danger btn-sm";
      btnDelete.textContent = "Hapus";
      btnDelete.addEventListener("click", () => {
        showConfirmDialog(
          "Hapus Kategori Permanen",
          `Apakah Anda yakin ingin menghapus kategori "${c.name}" secara permanen? Tindakan ini tidak dapat dibatalkan.`,
          async () => {
            try {
              const res = await API.deleteCategory(c.id);
              UI.showToast(res.message || `Kategori "${c.name}" berhasil dihapus`, "success");
              await loadCategories();
            } catch (err) {
              UI.showToast(err.message || "Gagal menghapus kategori", "error");
            }
          }
        );
      });

      btnGroup.append(btnToggle, btnDelete);
      tdAction.appendChild(btnGroup);
    } else {
      tdAction.textContent = "Sistem (Tetap)";
      tdAction.style.color = "var(--text-muted)";
      tdAction.style.fontSize = "12px";
    }

    tr.append(tdName, tdColor, tdDesc, tdType, tdStatus, tdAction);
    tbody.appendChild(tr);
  });
}

function renderManageCategoryTable(categories) {
  const tbody = document.getElementById("manageCategoryTableBody");
  if (!tbody) return;
  tbody.innerHTML = "";

  if (!categories || categories.length === 0) {
    tbody.innerHTML = '<tr><td colspan="5" class="empty-state">Belum ada data kategori.</td></tr>';
    return;
  }

  categories.forEach(c => {
    const tr = document.createElement("tr");

    // Nama Kategori (dengan badge warna)
    const tdName = document.createElement("td");
    const badge = document.createElement("span");
    badge.className = "category-badge-pill";
    badge.style.backgroundColor = c.color || "#0284c7";
    badge.textContent = c.name;
    tdName.appendChild(badge);

    // Deskripsi
    const tdDesc = document.createElement("td");
    tdDesc.textContent = c.description || "-";

    // Tipe
    const tdType = document.createElement("td");
    if (c.is_default) {
      tdType.innerHTML = '<span class="badge badge-status-completed"><span class="badge-dot"></span>Sistem</span>';
    } else {
      tdType.innerHTML = '<span class="badge badge-status-not_started"><span class="badge-dot"></span>Kustom</span>';
    }

    // Status
    const tdStatus = document.createElement("td");
    const badgeStatus = document.createElement("span");
    badgeStatus.className = `badge ${c.is_active ? "badge-status-completed" : "badge-status-cancelled"}`;
    const dot = document.createElement("span");
    dot.className = "badge-dot";
    badgeStatus.append(dot, document.createTextNode(c.is_active ? "Aktif" : "Nonaktif"));
    tdStatus.appendChild(badgeStatus);

    // Aksi
    const tdAction = document.createElement("td");
    tdAction.style.whiteSpace = "nowrap";

    const btnGroup = document.createElement("div");
    btnGroup.style.display = "flex";
    btnGroup.style.gap = "6px";

    const btnEdit = document.createElement("button");
    btnEdit.className = "btn btn-secondary btn-sm";
    btnEdit.textContent = "Edit";
    btnEdit.addEventListener("click", () => editCategoryInModal(c));
    btnGroup.appendChild(btnEdit);

    if (!c.is_default) {
      const btnToggle = document.createElement("button");
      btnToggle.className = "btn btn-secondary btn-sm";
      btnToggle.textContent = c.is_active ? "Nonaktifkan" : "Aktifkan";
      btnToggle.addEventListener("click", async () => {
        try {
          await API.updateCategory(c.id, {
            name: c.name,
            color: c.color,
            description: c.description,
            is_active: !c.is_active
          });
          const newStatus = !c.is_active ? "diaktifkan" : "dinonaktifkan";
          UI.showToast(`Kategori "${c.name}" berhasil ${newStatus}`, "info");
          await loadCategories();
          if (AppState.categoryModalCaller) {
            populateCategorySelects(null, AppState.categoryModalCaller);
          }
        } catch (err) {
          UI.showToast(`Gagal mengubah status kategori: ${err.message}`, "error");
        }
      });

      const btnDelete = document.createElement("button");
      btnDelete.className = "btn btn-danger btn-sm";
      btnDelete.textContent = "Hapus";
      btnDelete.addEventListener("click", () => {
        showConfirmDialog(
          "Hapus Kategori Permanen",
          `Apakah Anda yakin ingin menghapus kategori "${c.name}" secara permanen? Tindakan ini tidak dapat dibatalkan.`,
          async () => {
            try {
              const res = await API.deleteCategory(c.id);
              UI.showToast(res.message || `Kategori "${c.name}" berhasil dihapus`, "success");
              await loadCategories();
              if (AppState.categoryModalCaller) {
                populateCategorySelects(null, AppState.categoryModalCaller);
              }
            } catch (err) {
              UI.showToast(err.message || "Gagal menghapus kategori", "error");
            }
          }
        );
      });

      btnGroup.append(btnToggle, btnDelete);
    }

    tdAction.appendChild(btnGroup);
    tr.append(tdName, tdDesc, tdType, tdStatus, tdAction);
    tbody.appendChild(tr);
  });
}

function openManageCategoriesModal(sourceSelectId = null) {
  AppState.categoryModalCaller = sourceSelectId;
  resetCategoryForm();
  renderManageCategoryTable(AppState.allCategories);
  UI.openModal("modalManageCategories");
}

function resetCategoryForm() {
  const container = document.getElementById("categoryFormContainer");
  if (container) container.style.display = "none";
  const idEl = document.getElementById("manageCatId");
  if (idEl) idEl.value = "";
  const titleEl = document.getElementById("manageCatFormTitle");
  if (titleEl) titleEl.textContent = "Tambah Kategori Baru";
  const nameEl = document.getElementById("manageCatName");
  if (nameEl) {
    nameEl.value = "";
    nameEl.disabled = false;
  }
  const colorEl = document.getElementById("manageCatColor");
  if (colorEl) colorEl.value = "#0284c7";
  const descEl = document.getElementById("manageCatDesc");
  if (descEl) descEl.value = "";
  const groupActive = document.getElementById("groupManageCatActive");
  if (groupActive) groupActive.style.display = "none";
  const activeEl = document.getElementById("manageCatIsActive");
  if (activeEl) activeEl.checked = true;
}

function editCategoryInModal(cat) {
  const container = document.getElementById("categoryFormContainer");
  if (container) container.style.display = "block";
  const idEl = document.getElementById("manageCatId");
  if (idEl) idEl.value = cat.id;
  const titleEl = document.getElementById("manageCatFormTitle");
  if (titleEl) titleEl.textContent = `Edit Kategori: ${cat.name}`;
  const nameEl = document.getElementById("manageCatName");
  if (nameEl) {
    nameEl.value = cat.name;
    nameEl.disabled = !!cat.is_default;
  }
  const colorEl = document.getElementById("manageCatColor");
  if (colorEl) colorEl.value = cat.color || "#0284c7";
  const descEl = document.getElementById("manageCatDesc");
  if (descEl) descEl.value = cat.description || "";

  const groupActive = document.getElementById("groupManageCatActive");
  const activeEl = document.getElementById("manageCatIsActive");
  if (cat.is_default) {
    if (groupActive) groupActive.style.display = "none";
  } else {
    if (groupActive) groupActive.style.display = "block";
    if (activeEl) activeEl.checked = cat.is_active !== 0;
  }

  container.scrollIntoView({ behavior: "smooth", block: "nearest" });
}

// -------------------------------------------------------------
// 7. Modul Data Import Center (Sprint 3)
// -------------------------------------------------------------
function renderImportDataView() {
  if (AppState.importState.activeTab === "history") {
    loadImportHistory();
  }
}

function setupImportListeners() {
  // Tab switching: Wizard vs Riwayat
  document.querySelectorAll("[data-import-tab]").forEach(btn => {
    btn.addEventListener("click", () => {
      const tab = btn.getAttribute("data-import-tab");
      AppState.importState.activeTab = tab;

      document.querySelectorAll("[data-import-tab]").forEach(b => b.classList.remove("active"));
      btn.classList.add("active");

      const paneWizard = document.getElementById("tabContentImportWizard");
      const paneHistory = document.getElementById("tabContentImportHistory");

      if (tab === "wizard") {
        if (paneWizard) paneWizard.style.display = "block";
        if (paneHistory) paneHistory.style.display = "none";
      } else if (tab === "history") {
        if (paneWizard) paneWizard.style.display = "none";
        if (paneHistory) paneHistory.style.display = "block";
        loadImportHistory();
      }
    });
  });

  // Tombol Segarkan Riwayat
  const btnRefreshHist = document.getElementById("btnRefreshImportHistory");
  if (btnRefreshHist) {
    btnRefreshHist.addEventListener("click", () => loadImportHistory());
  }

  // File Picker & Dropzone
  const fileInput = document.getElementById("importFileInput");
  const btnSelectFile = document.getElementById("btnSelectImportFile");
  if (btnSelectFile && fileInput) {
    btnSelectFile.addEventListener("click", () => fileInput.click());
    fileInput.addEventListener("change", (e) => {
      if (e.target.files && e.target.files.length > 0) {
        handleImportFileSelect(e.target.files[0]);
      }
    });
  }

  // Step 1 -> Step 2
  const btnGoStep2 = document.getElementById("btnGoToStep2");
  if (btnGoStep2) {
    btnGoStep2.addEventListener("click", () => startImportInitialPreview());
  }

  // Step 2 -> Step 1
  const btnBackStep1 = document.getElementById("btnBackToStep1");
  if (btnBackStep1) {
    btnBackStep1.addEventListener("click", () => setImportWizardStep(1));
  }

  // Step 2 -> Dry Run Preview (Step 3)
  const btnRunDryRun = document.getElementById("btnRunDryRunPreview");
  if (btnRunDryRun) {
    btnRunDryRun.addEventListener("click", () => executeDryRunPreview());
  }

  // Step 3 -> Step 2 (Back)
  const btnBackStep2 = document.getElementById("btnBackToStep2");
  if (btnBackStep2) {
    btnBackStep2.addEventListener("click", () => setImportWizardStep(2));
  }

  // Step 3 -> Cancel
  const btnCancelImp = document.getElementById("btnCancelImport");
  if (btnCancelImp) {
    btnCancelImp.addEventListener("click", () => {
      resetImportWizard();
      setImportWizardStep(1);
    });
  }

  // Step 3 -> Commit
  const btnCommit = document.getElementById("btnCommitImport");
  if (btnCommit) {
    btnCommit.addEventListener("click", () => executeCommitImport());
  }

  // Step 4 Buttons
  const btnGoRegister = document.getElementById("btnFinishGoToRegister");
  if (btnGoRegister) {
    btnGoRegister.addEventListener("click", () => switchView("work_register"));
  }

  const btnNewImport = document.getElementById("btnFinishNewImport");
  if (btnNewImport) {
    btnNewImport.addEventListener("click", () => {
      resetImportWizard();
      setImportWizardStep(1);
    });
  }

  const btnViewHist = document.getElementById("btnFinishViewHistory");
  if (btnViewHist) {
    btnViewHist.addEventListener("click", () => {
      const histTabBtn = document.querySelector('[data-import-tab="history"]');
      if (histTabBtn) histTabBtn.click();
    });
  }
}

function handleImportFileSelect(file) {
  if (!file) return;

  const validExtensions = [".csv", ".xlsx"];
  const lowerName = file.name.toLowerCase();
  const isValidExt = validExtensions.some(ext => lowerName.endsWith(ext));

  if (!isValidExt) {
    UI.showToast("Format berkas tidak didukung. Harap pilih berkas .csv atau .xlsx", "error");
    return;
  }

  // 10 MB limit
  const maxBytes = 10 * 1024 * 1024;
  if (file.size > maxBytes) {
    UI.showToast("Ukuran berkas melebihi batas maksimal 10 MB", "error");
    return;
  }

  AppState.importState.selectedFile = file;
  AppState.importState.fileName = file.name;
  AppState.importState.fileFormat = lowerName.endsWith(".xlsx") ? "xlsx" : "csv";

  const infoEl = document.getElementById("selectedFileInfo");
  if (infoEl) {
    infoEl.style.display = "block";
    infoEl.textContent = `Berkas terpilih: ${file.name} (${UI.formatBytes(file.size)})`;
  }

  const btnGoStep2 = document.getElementById("btnGoToStep2");
  if (btnGoStep2) {
    btnGoStep2.disabled = false;
  }
}

function setImportWizardStep(step) {
  AppState.importState.currentStep = step;

  // Toggle Step Containers
  for (let s = 1; s <= 4; s++) {
    const el = document.getElementById(`importStep${s}`);
    if (el) {
      el.style.display = s === step ? "block" : "none";
    }
  }

  // Update Breadcrumbs
  for (let s = 1; s <= 4; s++) {
    const item = document.getElementById(`stepIndicator${s}`);
    const badge = s > 1 ? document.getElementById(`stepBadge${s}`) : null;
    if (item) {
      if (s === step) {
        item.style.fontWeight = "600";
        item.style.color = "var(--primary)";
        if (badge) {
          badge.style.backgroundColor = "var(--primary)";
          badge.style.color = "white";
        }
      } else if (s < step) {
        item.style.fontWeight = "500";
        item.style.color = "var(--success-text)";
        if (badge) {
          badge.style.backgroundColor = "var(--success-subtle)";
          badge.style.color = "var(--success-text)";
        }
      } else {
        item.style.fontWeight = "normal";
        item.style.color = "var(--text-muted)";
        if (badge) {
          badge.style.backgroundColor = "var(--bg-muted)";
          badge.style.color = "var(--text-muted)";
        }
      }
    }
  }
}

function resetImportWizard() {
  AppState.importState.selectedFile = null;
  AppState.importState.fileName = "";
  AppState.importState.fileFormat = "";
  AppState.importState.sheets = [];
  AppState.importState.selectedSheet = "";
  AppState.importState.headers = [];
  AppState.importState.suggestedMapping = {};
  AppState.importState.currentMapping = {};
  AppState.importState.previewResult = null;

  const fileInput = document.getElementById("importFileInput");
  if (fileInput) fileInput.value = "";

  const infoEl = document.getElementById("selectedFileInfo");
  if (infoEl) {
    infoEl.style.display = "none";
    infoEl.textContent = "";
  }

  const btnGoStep2 = document.getElementById("btnGoToStep2");
  if (btnGoStep2) btnGoStep2.disabled = true;

  const mappingTbody = document.getElementById("mappingTableBody");
  if (mappingTbody) mappingTbody.innerHTML = "";

  const previewTbody = document.getElementById("previewTableBody");
  if (previewTbody) previewTbody.innerHTML = "";
}

async function startImportInitialPreview() {
  const file = AppState.importState.selectedFile;
  if (!file) {
    UI.showToast("Pilih berkas terlebih dahulu", "error");
    return;
  }

  const btnGoStep2 = document.getElementById("btnGoToStep2");
  const origText = btnGoStep2 ? btnGoStep2.textContent : "";
  if (btnGoStep2) {
    btnGoStep2.disabled = true;
    btnGoStep2.textContent = "Menganalisis Berkas...";
  }

  try {
    const formData = new FormData();
    formData.append("file", file);
    if (AppState.importState.selectedSheet) {
      formData.append("sheet_name", AppState.importState.selectedSheet);
    }
    formData.append("duplicate_mode", "skip");

    const res = await API.previewImport(formData);

    AppState.importState.headers = res.detected_headers || [];
    AppState.importState.suggestedMapping = res.suggested_mapping || {};
    AppState.importState.sheets = res.worksheets || [];
    AppState.importState.selectedSheet = res.selected_worksheet || "";

    // Set Sheet Selector jika multi-sheet XLSX
    const groupSheet = document.getElementById("groupSheetSelect");
    const sheetSelect = document.getElementById("importSheetSelect");
    if (groupSheet && sheetSelect) {
      if (res.worksheets && res.worksheets.length > 1) {
        groupSheet.style.display = "block";
        sheetSelect.innerHTML = "";
        res.worksheets.forEach(sName => {
          const opt = document.createElement("option");
          opt.value = sName;
          opt.textContent = sName;
          if (sName === res.selected_worksheet) opt.selected = true;
          sheetSelect.appendChild(opt);
        });

        // Event listener saat ganti sheet
        sheetSelect.onchange = () => {
          AppState.importState.selectedSheet = sheetSelect.value;
          startImportInitialPreview();
        };
      } else {
        groupSheet.style.display = "none";
      }
    }

    // Set Meta Info
    const metaEl = document.getElementById("mappingFileMeta");
    if (metaEl) {
      const fmt = (res.file_type || "").toUpperCase();
      metaEl.textContent = `${res.file_name || ""} • Format: ${fmt} • Total: ${res.total_rows || 0} baris terdeteksi`;
    }

    // Render Mapping Table
    renderMappingTable(res.detected_headers || [], res.suggested_mapping || {});

    // Pindah ke Step 2
    setImportWizardStep(2);
  } catch (err) {
    UI.showToast(`Gagal menganalisis berkas: ${err.message}`, "error");
  } finally {
    if (btnGoStep2) {
      btnGoStep2.disabled = false;
      btnGoStep2.textContent = origText;
    }
  }
}

function renderMappingTable(detectedHeaders, suggestedMapping) {
  const tbody = document.getElementById("mappingTableBody");
  if (!tbody) return;
  tbody.innerHTML = "";

  const systemFields = [
    { key: "title", label: "Judul Pekerjaan", required: true, desc: "Teks judul kepatuhan yang akan dicatat." },
    { key: "received_on", label: "Tanggal Diterima", required: true, desc: "Format YYYY-MM-DD (contoh: 2026-10-17)." },
    { key: "category", label: "Kategori", required: false, desc: "Kategori kepatuhan (contoh: APU-PPT, General)." },
    { key: "requested_by", label: "Pemberi Tugas / Asal", required: false, desc: "Nama divisi / regulator / pihak pemohon." },
    { key: "due_on", label: "Batas Waktu (Tenggat)", required: false, desc: "Format YYYY-MM-DD (opsional)." },
    { key: "priority", label: "Prioritas", required: false, desc: "low, normal, high, critical (default: normal)." },
    { key: "status", label: "Status Pekerjaan", required: false, desc: "not_started, in_progress, waiting, completed, cancelled." },
    { key: "description", label: "Deskripsi Lengkap", required: false, desc: "Rincian tugas atau latar belakang." },
    { key: "notes", label: "Catatan Tambahan", required: false, desc: "Catatan internal audit." }
  ];

  systemFields.forEach(field => {
    const tr = document.createElement("tr");

    // Kolom Sistem
    const tdSys = document.createElement("td");
    const nameSpan = document.createElement("span");
    nameSpan.style.fontWeight = "600";
    nameSpan.textContent = field.label;
    tdSys.appendChild(nameSpan);

    if (field.required) {
      const star = document.createElement("span");
      star.style.color = "var(--danger)";
      star.style.marginLeft = "4px";
      star.textContent = "* (Wajib)";
      tdSys.appendChild(star);
    }

    // Kolom Header Berkas (Dropdown)
    const tdFile = document.createElement("td");
    const select = document.createElement("select");
    select.className = "form-control";
    select.setAttribute("data-map-field", field.key);

    const emptyOpt = document.createElement("option");
    emptyOpt.value = "";
    emptyOpt.textContent = "-- Abaikan Kolom Ini --";
    select.appendChild(emptyOpt);

    const preSelectedHeader = suggestedMapping[field.key] || "";

    detectedHeaders.forEach(hdr => {
      const opt = document.createElement("option");
      opt.value = hdr;
      opt.textContent = hdr;
      if (hdr.toLowerCase() === preSelectedHeader.toLowerCase()) {
        opt.selected = true;
      }
      select.appendChild(opt);
    });

    tdFile.appendChild(select);

    // Keterangan & Format
    const tdDesc = document.createElement("td");
    tdDesc.style.fontSize = "12px";
    tdDesc.style.color = "var(--text-muted)";
    tdDesc.textContent = field.desc;

    tr.append(tdSys, tdFile, tdDesc);
    tbody.appendChild(tr);
  });
}

function collectMappingFromUI() {
  const mapping = {};
  document.querySelectorAll("[data-map-field]").forEach(sel => {
    const fieldKey = sel.getAttribute("data-map-field");
    const val = sel.value.trim();
    if (val) {
      mapping[fieldKey] = val;
    }
  });
  return mapping;
}

async function executeDryRunPreview() {
  const file = AppState.importState.selectedFile;
  if (!file) {
    UI.showToast("Berkas belum dipilih", "error");
    return;
  }

  const mapping = collectMappingFromUI();
  if (!mapping.title || !mapping.received_on) {
    UI.showToast("Kolom 'Judul Pekerjaan' dan 'Tanggal Diterima' wajib dipetakan!", "error");
    return;
  }

  const dupModeEl = document.getElementById("importDuplicateMode");
  const duplicateMode = dupModeEl ? dupModeEl.value : "skip";
  AppState.importState.duplicateMode = duplicateMode;
  AppState.importState.currentMapping = mapping;

  const btnRun = document.getElementById("btnRunDryRunPreview");
  const origText = btnRun ? btnRun.textContent : "";
  if (btnRun) {
    btnRun.disabled = true;
    btnRun.textContent = "Menjalankan Validasi & Pratinjau...";
  }

  try {
    const formData = new FormData();
    formData.append("file", file);
    formData.append("mapping", JSON.stringify(mapping));
    formData.append("column_mapping", JSON.stringify(mapping));
    formData.append("duplicate_mode", duplicateMode);
    if (AppState.importState.selectedSheet) {
      formData.append("worksheet", AppState.importState.selectedSheet);
      formData.append("sheet_name", AppState.importState.selectedSheet);
    }

    const res = await API.previewImport(formData);
    if (!res || typeof res !== "object" || typeof res.total_rows !== "number") {
      throw new Error("Respons data pratinjau tidak valid dari server.");
    }
    AppState.importState.previewResult = res;

    // Render Preview Table & Cards
    renderPreviewTable(res);

    // Beralih ke Step 3
    setImportWizardStep(3);
  } catch (err) {
    UI.showToast(`Gagal menjalankan pratinjau: ${err.message}`, "error");
  } finally {
    if (btnRun) {
      btnRun.disabled = false;
      btnRun.textContent = origText;
    }
  }
}

function renderPreviewTable(result) {
  if (!result || typeof result !== "object") return;

  const total = result.total_rows || 0;
  const valid = result.valid_rows || 0;
  const duplicate = result.duplicate_rows || 0;
  const invalid = result.invalid_rows !== undefined ? result.invalid_rows : (result.error_rows || 0);

  // Update Metrics
  const mTotal = document.getElementById("previewMetricTotal");
  const mValid = document.getElementById("previewMetricValid");
  const mDuplicate = document.getElementById("previewMetricDuplicate");
  const mError = document.getElementById("previewMetricError");

  if (mTotal) mTotal.textContent = total;
  if (mValid) mValid.textContent = valid;
  if (mDuplicate) mDuplicate.textContent = duplicate;
  if (mError) mError.textContent = invalid;

  // Populate Table Body
  const tbody = document.getElementById("previewTableBody");
  if (!tbody) return;
  tbody.innerHTML = "";

  const rows = result.rows || result.sample_rows || [];
  if (rows.length === 0) {
    tbody.innerHTML = '<tr><td colspan="10" class="empty-state">Tidak ada baris data yang ditemukan dalam berkas.</td></tr>';
  } else {
    rows.forEach(r => {
      const tr = document.createElement("tr");

      // Baris ke-
      const tdRow = document.createElement("td");
      tdRow.style.fontWeight = "600";
      tdRow.textContent = r.row_number;

      // Status Validasi Badge
      const tdStatus = document.createElement("td");
      const statusBadge = document.createElement("span");
      const dot = document.createElement("span");
      dot.className = "badge-dot";

      if (!r.is_valid) {
        statusBadge.className = "badge badge-status-cancelled";
        statusBadge.append(dot, document.createTextNode("Error"));
      } else if (r.is_duplicate) {
        statusBadge.className = "badge badge-status-waiting";
        statusBadge.append(dot, document.createTextNode("Duplikasi"));
      } else {
        statusBadge.className = "badge badge-status-completed";
        statusBadge.append(dot, document.createTextNode("Siap Impor"));
      }
      tdStatus.appendChild(statusBadge);

      // Judul
      const tdTitle = document.createElement("td");
      tdTitle.style.maxWidth = "200px";
      tdTitle.textContent = r.title || "(Kosong)";

      // Kategori
      const tdCat = document.createElement("td");
      tdCat.textContent = r.category || "General";

      // Pemberi Tugas
      const tdReq = document.createElement("td");
      tdReq.textContent = r.requested_by || "-";

      // Tgl Diterima
      const tdRec = document.createElement("td");
      tdRec.textContent = r.received_on || "-";

      // Batas Waktu
      const tdDue = document.createElement("td");
      tdDue.textContent = r.due_on || "-";

      // Prioritas
      const tdPri = document.createElement("td");
      tdPri.appendChild(createPriorityBadge(r.priority || "normal"));

      // Status
      const tdStat = document.createElement("td");
      tdStat.appendChild(createStatusBadge(r.status || "not_started"));

      // Catatan Validasi
      const tdNotes = document.createElement("td");
      tdNotes.style.fontSize = "12px";

      const errs = r.validation_errors || r.errors || [];
      if (!r.is_valid && errs.length > 0) {
        tdNotes.style.color = "var(--danger)";
        tdNotes.textContent = errs.join("; ");
      } else if (r.is_duplicate) {
        tdNotes.style.color = "var(--warning-text)";
        tdNotes.textContent = r.duplicate_reason || "Duplikasi terdeteksi";
        if (r.matched_work_code) {
          tdNotes.textContent += ` (Sesuai ${r.matched_work_code})`;
        }
      } else {
        tdNotes.style.color = "var(--success-text)";
        tdNotes.textContent = "Data valid";
      }

      tr.append(tdRow, tdStatus, tdTitle, tdCat, tdReq, tdRec, tdDue, tdPri, tdStat, tdNotes);
      tbody.appendChild(tr);
    });
  }

  // Atur Tombol Commit (selalu dipanggil agar status tombol sinkron)
  const isSkipMode = (AppState.importState.duplicateMode === "skip");
  const importableCount = isSkipMode ? valid : (valid + duplicate);

  const btnCommit = document.getElementById("btnCommitImport");
  const labelCommit = document.getElementById("btnCommitLabel");

  if (btnCommit && labelCommit) {
    if (importableCount > 0) {
      btnCommit.disabled = false;
      labelCommit.textContent = `Komit & Simpan Impor (${importableCount} Pekerjaan)`;
    } else {
      btnCommit.disabled = true;
      labelCommit.textContent = "Tidak Ada Baris Valid untuk Diimpor";
    }
  }
}

async function executeCommitImport() {
  const file = AppState.importState.selectedFile;
  const mapping = AppState.importState.currentMapping;
  const duplicateMode = AppState.importState.duplicateMode || "skip";
  const result = AppState.importState.previewResult;

  if (!file || !mapping || !result) {
    UI.showToast("Data impor tidak lengkap", "error");
    return;
  }

  const isSkipMode = (duplicateMode === "skip");
  const validRows = result.valid_rows || 0;
  const dupRows = result.duplicate_rows || 0;
  const importableCount = isSkipMode ? validRows : (validRows + dupRows);

  if (importableCount <= 0) {
    UI.showToast("Tidak ada data yang dapat diimpor", "error");
    return;
  }

  showConfirmDialog(
    "Konfirmasi Komit Impor Data",
    `Apakah Anda yakin ingin memproses impor ${importableCount} data pekerjaan ke dalam database? Salinan cadangan otomatis (pre-import snapshot) akan dibuat demi keamanan sebelum transaksi commit dijalankan.`,
    async () => {
      const btnCommit = document.getElementById("btnCommitImport");
      const origText = btnCommit ? btnCommit.innerHTML : "";
      if (btnCommit) {
        btnCommit.disabled = true;
        btnCommit.textContent = "Memproses Impor & Snapshot Cadangan...";
      }

      try {
        const formData = new FormData();
        formData.append("file", file);
        formData.append("mapping", JSON.stringify(mapping));
        formData.append("column_mapping", JSON.stringify(mapping));
        formData.append("duplicate_mode", duplicateMode);
        if (AppState.importState.selectedSheet) {
          formData.append("worksheet", AppState.importState.selectedSheet);
          formData.append("sheet_name", AppState.importState.selectedSheet);
        }

        const res = await API.commitImport(formData);

        UI.showToast("Impor data berhasil diproses!", "success");

        // Tampilkan Ringkasan Hasil di Step 4
        const summaryEl = document.getElementById("importResultSummaryText");
        if (summaryEl) {
          const created = res.imported_rows !== undefined ? res.imported_rows : (res.created_count || 0);
          const skipped = res.skipped_rows !== undefined ? res.skipped_rows : (res.skipped_count || 0);
          const failed = res.failed_rows !== undefined ? res.failed_rows : (res.failed_count || 0);
          summaryEl.textContent = `Batch #${res.batch_id} selesai. Berhasil membuat ${created} pekerjaan baru, melewati ${skipped} baris duplikasi, dan mencatat ${failed} baris error.`;
        }

        // Pindah ke Step 4
        setImportWizardStep(4);

        // Muat ulang data pekerjaan
        await loadWorkItems();
      } catch (err) {
        UI.showToast(`Gagal melakukan impor: ${err.message}`, "error");
      } finally {
        if (btnCommit) {
          btnCommit.disabled = false;
          btnCommit.innerHTML = origText;
        }
      }
    }
  );
}

async function loadImportHistory() {
  const tbody = document.getElementById("importHistoryTableBody");
  if (!tbody) return;
  tbody.innerHTML = '<tr><td colspan="10" class="empty-state"><div class="spinner"></div><div>Memuat riwayat impor...</div></td></tr>';

  try {
    const res = await API.listImportHistory(50, 0);
    const batches = (res && res.batches) ? res.batches : [];
    renderImportHistoryTable(batches);
  } catch (err) {
    tbody.innerHTML = `<tr><td colspan="10" class="empty-state" style="color: var(--danger);">Gagal memuat riwayat: ${err.message}</td></tr>`;
  }
}

function renderImportHistoryTable(batches) {
  const tbody = document.getElementById("importHistoryTableBody");
  if (!tbody) return;
  tbody.innerHTML = "";

  if (!batches || batches.length === 0) {
    tbody.innerHTML = '<tr><td colspan="10" class="empty-state">Belum ada riwayat proses impor.</td></tr>';
    return;
  }

  batches.forEach(b => {
    const tr = document.createElement("tr");

    // ID Batch
    const tdID = document.createElement("td");
    const badgeID = document.createElement("span");
    badgeID.className = "work-code-badge";
    badgeID.textContent = `#${b.id}`;
    tdID.appendChild(badgeID);

    // Waktu Impor
    const tdTime = document.createElement("td");
    tdTime.textContent = UI.formatDateTimeID(b.created_at);

    // Nama Berkas
    const tdFile = document.createElement("td");
    tdFile.style.fontWeight = "600";
    tdFile.textContent = b.file_name;

    // Format
    const tdFmt = document.createElement("td");
    const fmtBadge = document.createElement("span");
    fmtBadge.className = "badge badge-priority-normal";
    fmtBadge.textContent = (b.file_type || b.file_format || "").toUpperCase();
    tdFmt.appendChild(fmtBadge);

    // Total
    const tdTotal = document.createElement("td");
    tdTotal.textContent = b.total_rows;

    const createdCount = b.imported_rows !== undefined ? b.imported_rows : (b.created_count || 0);
    const skippedCount = b.skipped_rows !== undefined ? b.skipped_rows : (b.skipped_count || 0);
    const failedCount = b.failed_rows !== undefined ? b.failed_rows : (b.failed_count || 0);

    // Dibuat
    const tdCreated = document.createElement("td");
    tdCreated.innerHTML = `<span class="badge badge-status-completed">${createdCount}</span>`;

    // Dilewati
    const tdSkipped = document.createElement("td");
    tdSkipped.innerHTML = `<span class="badge badge-status-waiting">${skippedCount}</span>`;

    // Gagal
    const tdFailed = document.createElement("td");
    if (failedCount > 0) {
      tdFailed.innerHTML = `<span class="badge badge-status-cancelled">${failedCount}</span>`;
    } else {
      tdFailed.innerHTML = `<span class="badge badge-status-not_started">0</span>`;
    }

    // Status Batch
    const tdStatus = document.createElement("td");
    const stBadge = document.createElement("span");
    const dot = document.createElement("span");
    dot.className = "badge-dot";

    if (b.status === "completed") {
      stBadge.className = "badge badge-status-completed";
      stBadge.append(dot, document.createTextNode("Selesai"));
    } else if (b.status === "rolled_back") {
      stBadge.className = "badge badge-status-cancelled";
      stBadge.append(dot, document.createTextNode("Dibatalkan (Kompensasi)"));
    } else if (b.status === "failed") {
      stBadge.className = "badge badge-status-cancelled";
      stBadge.append(dot, document.createTextNode("Gagal"));
    } else {
      stBadge.className = "badge badge-status-waiting";
      stBadge.append(dot, document.createTextNode(b.status));
    }
    tdStatus.appendChild(stBadge);

    // Aksi
    const tdActions = document.createElement("td");
    const actionWrap = document.createElement("div");
    actionWrap.style.display = "flex";
    actionWrap.style.gap = "6px";
    actionWrap.style.flexWrap = "wrap";

    // Tombol Detail
    const btnDetail = document.createElement("button");
    btnDetail.className = "btn btn-secondary btn-sm";
    btnDetail.textContent = "Detail";
    btnDetail.addEventListener("click", () => showImportBatchDetailModal(b.id));
    actionWrap.appendChild(btnDetail);

    // Tombol Unduh Laporan Error (jika ada baris gagal)
    if (failedCount > 0) {
      const btnError = document.createElement("a");
      btnError.className = "btn btn-secondary btn-sm";
      btnError.href = API.getImportErrorReportUrl(b.id);
      btnError.download = `error_report_batch_${b.id}.csv`;
      btnError.textContent = "Laporan Error";
      btnError.title = "Unduh daftar baris gagal beserta alasan validasi";
      actionWrap.appendChild(btnError);
    }

    // Tombol Kompensasi Pembatalan (jika berstatus completed dan membuat pekerjaan)
    if (b.status === "completed" && createdCount > 0) {
      const btnCompensate = document.createElement("button");
      btnCompensate.className = "btn btn-secondary btn-sm";
      btnCompensate.style.color = "var(--danger)";
      btnCompensate.textContent = "Batalkan";
      btnCompensate.title = "Batalkan impor dengan tindakan kompensasi (mengarsipkan seluruh pekerjaan batch ini)";
      btnCompensate.addEventListener("click", () => compensateImportBatch(b.id, createdCount));
      actionWrap.appendChild(btnCompensate);
    }

    tdActions.appendChild(actionWrap);

    tr.append(tdID, tdTime, tdFile, tdFmt, tdTotal, tdCreated, tdSkipped, tdFailed, tdStatus, tdActions);
    tbody.appendChild(tr);
  });
}

async function showImportBatchDetailModal(batchId) {
  try {
    const res = await API.getImportBatch(batchId);
    const b = res.batch;
    const items = res.items || [];

    const titleEl = document.getElementById("importBatchDetailTitle");
    if (titleEl) {
      titleEl.textContent = `Detail Riwayat Impor Batch #${b.id} (${b.file_name})`;
    }

    const metaEl = document.getElementById("importBatchMetaInfo");
    if (metaEl) {
      const createdCount = b.imported_rows !== undefined ? b.imported_rows : (b.created_count || 0);
      const skippedCount = b.skipped_rows !== undefined ? b.skipped_rows : (b.skipped_count || 0);
      const failedCount = b.failed_rows !== undefined ? b.failed_rows : (b.failed_count || 0);
      const fmt = (b.file_type || b.file_format || "").toUpperCase();

      metaEl.innerHTML = `
        <div style="display: flex; gap: 16px; flex-wrap: wrap;">
          <div><strong>Waktu Impor:</strong> ${UI.formatDateTimeID(b.created_at)}</div>
          <div><strong>Format:</strong> ${fmt}</div>
          <div><strong>Total:</strong> ${b.total_rows} baris</div>
          <div><strong>Dibuat:</strong> <span class="badge badge-status-completed">${createdCount}</span></div>
          <div><strong>Dilewati:</strong> <span class="badge badge-status-waiting">${skippedCount}</span></div>
          <div><strong>Gagal:</strong> <span class="badge ${failedCount > 0 ? 'badge-status-cancelled' : 'badge-status-not_started'}">${failedCount}</span></div>
          <div><strong>Status:</strong> ${b.status}</div>
        </div>
      `;
    }

    const tbody = document.getElementById("importBatchItemsTableBody");
    if (tbody) {
      tbody.innerHTML = "";
      if (items.length === 0) {
        tbody.innerHTML = '<tr><td colspan="6" class="empty-state">Tidak ada rincian item baris.</td></tr>';
      } else {
        items.forEach(it => {
          const tr = document.createElement("tr");

          // Baris
          const tdRow = document.createElement("td");
          tdRow.textContent = it.row_number;

          // Status Item
          const tdStat = document.createElement("td");
          const stBadge = document.createElement("span");
          const dot = document.createElement("span");
          dot.className = "badge-dot";

          if (it.status === "created") {
            stBadge.className = "badge badge-status-completed";
            stBadge.append(dot, document.createTextNode("Dibuat"));
          } else if (it.status === "skipped") {
            stBadge.className = "badge badge-status-waiting";
            stBadge.append(dot, document.createTextNode("Dilewati"));
          } else {
            stBadge.className = "badge badge-status-cancelled";
            stBadge.append(dot, document.createTextNode("Gagal"));
          }
          tdStat.appendChild(stBadge);

          // Kode / Judul
          const tdTitle = document.createElement("td");
          if (it.work_code) {
            const codeSpan = document.createElement("span");
            codeSpan.className = "work-code-badge";
            codeSpan.style.marginRight = "6px";
            codeSpan.textContent = it.work_code;
            tdTitle.appendChild(codeSpan);
          }
          tdTitle.appendChild(document.createTextNode(it.title || "-"));

          // Pemberi Tugas
          const tdReq = document.createElement("td");
          tdReq.textContent = it.requested_by || "-";

          // Tgl Diterima
          const tdRec = document.createElement("td");
          tdRec.textContent = it.received_on || "-";

          // Catatan / Pesan
          const tdNote = document.createElement("td");
          tdNote.style.fontSize = "12px";
          if (it.status === "failed") {
            tdNote.style.color = "var(--danger)";
            tdNote.textContent = it.error_message || "Validasi gagal";
          } else if (it.status === "skipped") {
            tdNote.style.color = "var(--warning-text)";
            tdNote.textContent = it.error_message || "Duplikasi dilewati";
          } else {
            tdNote.style.color = "var(--success-text)";
            tdNote.textContent = "Sukses dibuat";
          }

          tr.append(tdRow, tdStat, tdTitle, tdReq, tdRec, tdNote);
          tbody.appendChild(tr);
        });
      }
    }

    UI.openModal("modalImportBatchDetail");
  } catch (err) {
    UI.showToast(`Gagal memuat detail batch: ${err.message}`, "error");
  }
}

function compensateImportBatch(batchId, createdCount) {
  showConfirmDialog(
    "Konfirmasi Kompensasi Pembatalan Impor",
    `PERINGATAN: Pembatalan impor batch #${batchId} akan mengarsipkan seluruh ${createdCount} pekerjaan kepatuhan yang dibuat oleh batch ini dan mencatat tindakan kompensasi pada Activity Log audit trail. Lanjutkan?`,
    async () => {
      try {
        const res = await API.compensateBatch(batchId, false);
        UI.showToast(`Kompensasi berhasil: ${res.compensated_count} pekerjaan diarsipkan`, "success");
        await loadImportHistory();
        await loadWorkItems();
      } catch (err) {
        if (err.has_edited_items) {
          showConfirmDialog(
            "Peringatan Khusus: Pekerjaan Telah Diedit",
            `PERINGATAN KERAS: Terdapat ${err.edited_count} pekerjaan pada batch ini yang telah diedit atau diubah statusnya setelah impor!\n\nApakah Anda benar-benar yakin ingin tetap mengarsipkan pekerjaan yang telah dimodifikasi tersebut?`,
            async () => {
              try {
                const forceRes = await API.compensateBatch(batchId, true);
                UI.showToast(`Kompensasi paksa berhasil: ${forceRes.compensated_count} pekerjaan diarsipkan`, "success");
                await loadImportHistory();
                await loadWorkItems();
              } catch (forceErr) {
                UI.showToast(`Gagal melakukan kompensasi: ${forceErr.message}`, "error");
              }
            }
          );
        } else {
          UI.showToast(`Gagal melakukan kompensasi: ${err.message}`, "error");
        }
      }
    }
  );
}
