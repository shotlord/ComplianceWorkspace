/**
 * Compliance Workspace v1.0 — API Client
 * Menggunakan Fetch API dengan relative URL dan proteksi CSRF otomatis.
 */
const API = (() => {
  let cachedCsrfToken = null;

  async function fetchCsrfToken() {
    try {
      const res = await fetch("/api/csrf-token", { credentials: "same-origin" });
      if (!res.ok) {
        throw new Error("Gagal mengambil token CSRF dari server");
      }
      const data = await res.json();
      cachedCsrfToken = data.csrf_token;
      return cachedCsrfToken;
    } catch (err) {
      console.error("Kesalahan fetchCsrfToken:", err);
      throw err;
    }
  }

  async function request(endpoint, options = {}) {
    const method = (options.method || "GET").toUpperCase();
    const headers = {
      "Accept": "application/json",
      ...(options.headers || {})
    };

    // Tambahkan token CSRF untuk metode mutasi data
    if (method !== "GET" && method !== "HEAD") {
      if (!cachedCsrfToken) {
        await fetchCsrfToken();
      }
      headers["X-CSRF-Token"] = cachedCsrfToken;
    }

    if (options.body && typeof options.body === "object" && !(options.body instanceof FormData)) {
      headers["Content-Type"] = "application/json";
      options.body = JSON.stringify(options.body);
    }

    const config = {
      ...options,
      method,
      headers,
      credentials: "same-origin"
    };

    let res = await fetch(endpoint, config);

    // Jika terjadi 403 (mungkin token CSRF kedaluwarsa atau belum sinkron), coba perbarui token 1 kali
    if (res.status === 403 && method !== "GET") {
      try {
        const refreshedToken = await fetchCsrfToken();
        headers["X-CSRF-Token"] = refreshedToken;
        config.headers = headers;
        res = await fetch(endpoint, config);
      } catch {
        // Biarkan fallback ke penanganan error di bawah jika retry gagal
      }
    }

    let responseData = null;
    const contentType = res.headers.get("content-type");
    if (contentType && contentType.includes("application/json")) {
      responseData = await res.json();
    } else {
      const text = await res.text();
      responseData = { message: text };
    }

    if (!res.ok) {
      const errorMsg = responseData?.error || responseData?.message || `Permintaan gagal (${res.status})`;
      const err = new Error(errorMsg);
      if (responseData && typeof responseData === "object") {
        Object.assign(err, responseData);
      }
      throw err;
    }

    return responseData;
  }

  return {
    init: fetchCsrfToken,

    // Work Item Endpoints
    listWorkItems: (params = {}) => {
      const query = new URLSearchParams();
      if (params.status) query.set("status", params.status);
      if (params.priority) query.set("priority", params.priority);
      if (params.category) query.set("category", params.category);
      if (params.search) query.set("search", params.search);
      if (params.include_archived) query.set("include_archived", "true");
      if (params.provenance) query.set("provenance", params.provenance);
      if (params.sort_by) query.set("sort_by", params.sort_by);
      if (params.sort_order) query.set("sort_order", params.sort_order);

      const qs = query.toString();
      return request(`/api/work-items${qs ? "?" + qs : ""}`);
    },

    getWorkItem: (id) => request(`/api/work-items/${encodeURIComponent(id)}`),

    createWorkItem: (payload) => request("/api/work-items", {
      method: "POST",
      body: payload
    }),

    updateWorkItem: (id, payload) => request(`/api/work-items/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: payload
    }),

    archiveWorkItem: (id) => request(`/api/work-items/${encodeURIComponent(id)}/archive`, {
      method: "POST"
    }),

    restoreWorkItem: (id) => request(`/api/work-items/${encodeURIComponent(id)}/restore`, {
      method: "POST"
    }),

    // Activity Log Endpoints
    listActivities: (workItemId) => request(`/api/work-items/${encodeURIComponent(workItemId)}/activities`),

    addActivity: (workItemId, payload) => request(`/api/work-items/${encodeURIComponent(workItemId)}/activities`, {
      method: "POST",
      body: payload
    }),

    // Backup & Restore Endpoints
    listBackups: () => request("/api/backups"),

    createBackup: () => request("/api/backups", {
      method: "POST"
    }),

    restoreBackup: (filename) => request("/api/backups/restore", {
      method: "POST",
      body: { filename }
    }),

    // Subtask & Checklist Endpoints (Sprint 2)
    listSubtasks: (workItemId) => request(`/api/work-items/${encodeURIComponent(workItemId)}/subtasks`),

    createSubtask: (workItemId, payload) => request(`/api/work-items/${encodeURIComponent(workItemId)}/subtasks`, {
      method: "POST",
      body: payload
    }),

    updateSubtask: (id, payload) => request(`/api/subtasks/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: payload
    }),

    deleteSubtask: (id) => request(`/api/subtasks/${encodeURIComponent(id)}`, {
      method: "DELETE"
    }),

    // Categories Endpoints
    listCategories: (all = false) => request(`/api/categories${all ? "?all=true" : ""}`),

    createCategory: (payload) => request("/api/categories", {
      method: "POST",
      body: payload
    }),

    updateCategory: (id, payload) => request(`/api/categories/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: payload
    }),

    deleteCategory: (id) => request(`/api/categories/${encodeURIComponent(id)}`, {
      method: "DELETE"
    }),

    // Work Templates Endpoints
    listTemplates: () => request("/api/templates"),

    getTemplate: (id) => request(`/api/templates/${encodeURIComponent(id)}`),

    createTemplate: (payload) => request("/api/templates", {
      method: "POST",
      body: payload
    }),

    updateTemplate: (id, payload) => request(`/api/templates/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: payload
    }),

    deleteTemplate: (id) => request(`/api/templates/${encodeURIComponent(id)}`, {
      method: "DELETE"
    }),

    applyTemplate: (id, payload) => {
      const body = typeof payload === "string" ? { received_on: payload } : (payload || {});
      return request(`/api/templates/${encodeURIComponent(id)}/apply`, {
        method: "POST",
        body
      });
    },

    // Recurring Tasks Endpoints
    listRecurringSchedules: () => request("/api/recurring-schedules"),

    createRecurringSchedule: (payload) => request("/api/recurring-schedules", {
      method: "POST",
      body: payload
    }),

    updateRecurringSchedule: (id, payload) => request(`/api/recurring-schedules/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: payload
    }),

    deleteRecurringSchedule: (id) => request(`/api/recurring-schedules/${encodeURIComponent(id)}`, {
      method: "DELETE"
    }),

    triggerRecurring: (asOfDate) => request("/api/recurring-schedules/trigger", {
      method: "POST",
      body: { as_of_date: asOfDate || null }
    }),

    // Data Import Center Endpoints (Sprint 3)
    previewImport: (formData) => request("/api/import/preview", {
      method: "POST",
      body: formData
    }),

    commitImport: (formData) => request("/api/import/commit", {
      method: "POST",
      body: formData
    }),

    listImportHistory: (limit = 20, offset = 0) => request(`/api/import/history?limit=${encodeURIComponent(limit)}&offset=${encodeURIComponent(offset)}`),

    getImportBatch: (id) => request(`/api/import/batches/${encodeURIComponent(id)}`),

    compensateBatch: (id, force = false) => request(`/api/import/batches/${encodeURIComponent(id)}/compensate${force ? "?force=true" : ""}`, {
      method: "POST"
    }),

    getImportErrorReportUrl: (id) => `/api/import/batches/${encodeURIComponent(id)}/error-report`,
    getCSVTemplateUrl: () => "/api/import/template/csv",
    getXLSXTemplateUrl: () => "/api/import/template/xlsx"
  };
})();
