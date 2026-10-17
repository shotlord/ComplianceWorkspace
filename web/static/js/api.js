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
      throw new Error(errorMsg);
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
    })
  };
})();
