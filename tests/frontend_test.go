package tests

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrontendEmbeddedAssetsServing(t *testing.T) {
	_, baseURL, cleanup := setupTestServer(t)
	defer cleanup()

	client := &http.Client{}

	// 1. Uji GET / (index.html)
	resp, err := client.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("Gagal mengakses root URL: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Ekspektasi status 200 OK untuk /, didapat: %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("Content-Type untuk / harus text/html, didapat: %s", contentType)
	}

	// Periksa header keamanan
	if resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Errorf("X-Frame-Options harus DENY")
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("X-Content-Type-Options harus nosniff")
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Errorf("Content-Security-Policy harus diset")
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Gagal membaca body HTML: %v", err)
	}
	bodyStr := string(bodyBytes)

	// Pastikan elemen utama ada di halaman
	requiredSnippets := []string{
		"Compliance Workspace",
		"Dashboard",
		"Daftar Pekerjaan",
		"Riwayat Aktivitas",
		"Pemantauan Tenggat",
		"Pengaturan dan Cadangan",
		"modalCreateWork",
		"modalEditWork",
		"modalWorkDetail",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(bodyStr, snippet) {
			t.Errorf("Halaman index.html tidak memuat cuplikan yang diharapkan: '%s'", snippet)
		}
	}

	// 2. Uji GET /css/app.css
	respCSS, err := client.Get(baseURL + "/css/app.css")
	if err != nil {
		t.Fatalf("Gagal memuat CSS: %v", err)
	}
	defer respCSS.Body.Close()
	if respCSS.StatusCode != http.StatusOK {
		t.Errorf("Ekspektasi 200 OK untuk /css/app.css, didapat: %d", respCSS.StatusCode)
	}
	if !strings.Contains(respCSS.Header.Get("Content-Type"), "text/css") {
		t.Errorf("Content-Type CSS salah: %s", respCSS.Header.Get("Content-Type"))
	}

	// 3. Uji GET /js/api.js
	respAPI, err := client.Get(baseURL + "/js/api.js")
	if err != nil {
		t.Fatalf("Gagal memuat api.js: %v", err)
	}
	defer respAPI.Body.Close()
	if respAPI.StatusCode != http.StatusOK {
		t.Errorf("Ekspektasi 200 OK untuk /js/api.js, didapat: %d", respAPI.StatusCode)
	}

	// 4. Uji GET /js/app.js
	respApp, err := client.Get(baseURL + "/js/app.js")
	if err != nil {
		t.Fatalf("Gagal memuat app.js: %v", err)
	}
	defer respApp.Body.Close()
	if respApp.StatusCode != http.StatusOK {
		t.Errorf("Ekspektasi 200 OK untuk /js/app.js, didapat: %d", respApp.StatusCode)
	}
}

func TestUXWritingConsistency(t *testing.T) {
	readAsset := func(parts ...string) string {
		path := filepath.Join(append([]string{"..", "web", "static"}, parts...)...)
		content, err := os.ReadFile(path)
		if err != nil {
			path = filepath.Join(append([]string{"web", "static"}, parts...)...)
			content, err = os.ReadFile(path)
		}
		if err != nil {
			t.Fatalf("Gagal membaca aset frontend %s: %v", path, err)
		}
		return string(content)
	}

	indexHTML := readAsset("index.html")
	appJS := readAsset("js", "app.js")
	allCopy := indexHTML + "\n" + appJS

	requiredCopy := []string{
		"Daftar Pekerjaan",
		"Riwayat Aktivitas",
		"Pemantauan Tenggat",
		"Pengaturan dan Cadangan",
		"Jadwal aktif yang dipantau.",
		"Pilih pekerjaan untuk melihat riwayat aktivitasnya.",
		"Jadwal berulang diproses saat aplikasi dibuka dan setiap jam.",
		"Cadangan database disimpan di folder <code>backups/</code>",
		`Klik "Tambah Pekerjaan" untuk mencatat tugas baru.`,
		`Klik "Periksa Jadwal" untuk memeriksa jadwal sekarang.`,
		`Klik "Buat Backup Sekarang" untuk membuatnya.`,
		`Klik "Tambah Jadwal" untuk membuatnya.`,
		`Klik "Tambah Template" untuk membuatnya.`,
	}
	for _, copy := range requiredCopy {
		if !strings.Contains(allCopy, copy) {
			t.Errorf("Teks UI wajib tidak ditemukan: %q", copy)
		}
	}

	buttonLabels := []string{
		"Tambah Pekerjaan",
		"Periksa Jadwal",
		"Buat Backup Sekarang",
		"Tambah Jadwal",
		"Template CSV",
		"Template XLSX",
	}
	for _, label := range buttonLabels {
		if !strings.Contains(indexHTML, label) {
			t.Errorf("Label tombol yang dirujuk tidak ditemukan pada HTML: %q", label)
		}
	}

	legacyCopy := []string{
		"Tidak Ada Pekerjaan Terlambat",
		`Gunakan tombol "Tambah Pekerjaan"`,
		"via background ticker",
		"Segera/siap dievaluasi",
		"Kategori nonaktif / expired",
	}
	for _, copy := range legacyCopy {
		if strings.Contains(allCopy, copy) {
			t.Errorf("Teks UI lama masih ditemukan: %q", copy)
		}
	}
}

// TestJavaScriptSyntaxValidation memeriksa keabsahan sintaks seluruh file JavaScript frontend (Regression Test)
func TestJavaScriptSyntaxValidation(t *testing.T) {
	// Temukan path folder aset frontend
	basePath := "../web/static/js"
	if _, err := os.Stat(basePath); err != nil {
		basePath = "web/static/js"
	}

	jsFiles := []string{
		filepath.Join(basePath, "api.js"),
		filepath.Join(basePath, "app.js"),
	}

	// 1. Jika runtime Node.js terpasang, jalankan node --check
	nodePath, err := exec.LookPath("node")
	if err == nil && nodePath != "" {
		for _, file := range jsFiles {
			cmd := exec.Command(nodePath, "--check", file)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Kesalahan sintaks JavaScript terdeteksi pada %s: %s (error: %v)", file, string(output), err)
			}
		}
	}

	// 2. Pemeriksaan keseimbangan kurung kurawal dasar untuk proteksi ganda
	for _, file := range jsFiles {
		contentBytes, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("Gagal membaca file %s: %v", file, err)
		}
		content := string(contentBytes)

		braceDepth := 0
		inString := false
		var stringQuote rune
		inLineComment := false
		inBlockComment := false
		escaped := false

		runes := []rune(content)
		for i := 0; i < len(runes); i++ {
			r := runes[i]

			if inLineComment {
				if r == '\n' {
					inLineComment = false
				}
				continue
			}

			if inBlockComment {
				if r == '*' && i+1 < len(runes) && runes[i+1] == '/' {
					inBlockComment = false
					i++
				}
				continue
			}

			if inString {
				if escaped {
					escaped = false
					continue
				}
				if r == '\\' {
					escaped = true
					continue
				}
				if r == stringQuote {
					inString = false
				}
				continue
			}

			// Cek komentar
			if r == '/' && i+1 < len(runes) {
				if runes[i+1] == '/' {
					inLineComment = true
					i++
					continue
				}
				if runes[i+1] == '*' {
					inBlockComment = true
					i++
					continue
				}
			}

			// Cek string
			if r == '"' || r == '\'' || r == '`' {
				inString = true
				stringQuote = r
				escaped = false
				continue
			}

			if r == '{' {
				braceDepth++
			} else if r == '}' {
				braceDepth--
				if braceDepth < 0 {
					t.Fatalf("Kelebihan kurung tutup '}' pada file %s", file)
				}
			}
		}

		if braceDepth != 0 {
			t.Fatalf("Kurung kurawal tidak seimbang pada %s: kedalaman kurung tersisa = %d", file, braceDepth)
		}
	}
}

func TestDeadlineTrackerSevenDayCategorization(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js tidak tersedia untuk menjalankan regression test Deadline Tracker")
	}

	appPath := filepath.Join("..", "web", "static", "js", "app.js")
	appJS, err := os.ReadFile(appPath)
	if err != nil {
		appPath = filepath.Join("web", "static", "js", "app.js")
		appJS, err = os.ReadFile(appPath)
	}
	if err != nil {
		t.Fatalf("Gagal membaca app.js: %v", err)
	}

	source := string(appJS)
	start := strings.Index(source, "function renderDeadlineTracker()")
	if start == -1 {
		t.Fatal("Fungsi renderDeadlineTracker tidak ditemukan")
	}
	endOffset := strings.Index(source[start:], "function createDeadlineItemCard")
	if endOffset == -1 {
		t.Fatal("Batas akhir fungsi renderDeadlineTracker tidak ditemukan")
	}
	renderFunction := source[start : start+endOffset]

	harness := `
"use strict";
const assert = require("assert");
const AppState = {
  workItems: [
    { work_code: "OVERDUE", due_on: "2026-10-19", status: "in_progress" },
    { work_code: "TODAY", due_on: "2026-10-20", status: "not_started" },
    { work_code: "UPCOMING-START", due_on: "2026-10-21", status: "waiting" },
    { work_code: "UPCOMING-END", due_on: "2026-10-27", status: "in_progress" },
    { work_code: "BEYOND-ONE", due_on: "2026-10-28", status: "not_started" },
    { work_code: "BEYOND-FAR", due_on: "2026-10-31", status: "not_started" },
    { work_code: "NO-DUE", due_on: "", status: "waiting" },
    { work_code: "COMPLETED", due_on: "2026-10-21", status: "completed" },
    { work_code: "CANCELLED", due_on: "2026-10-21", status: "cancelled" },
    { work_code: "ARCHIVED", due_on: "2026-10-21", status: "in_progress", archived_at: "2026-10-20T00:00:00Z" }
  ]
};
function makeNode() {
  let html = "";
  return {
    children: [],
    textContent: "",
    get innerHTML() { return html; },
    set innerHTML(value) { html = value; if (value === "") this.children = []; },
    appendChild(child) { this.children.push(child); }
  };
}
const elements = {};
["colOverdueItems", "colTodayItems", "colUpcomingItems", "colNoneItems", "countOverdue", "countToday", "countUpcoming", "countNone"]
  .forEach(id => { elements[id] = makeNode(); });
const document = { getElementById(id) { return elements[id] || null; } };
const UI = {
  getTodayYYYYMMDD() { return "2026-10-20"; },
  getDateOffsetYYYYMMDD(days) {
    assert.strictEqual(days, 7);
    return "2026-10-27";
  }
};
function createDeadlineItemCard(item, urgency) {
  return { workCode: item.work_code, urgency: urgency || "" };
}
` + renderFunction + `
renderDeadlineTracker();
const codes = id => elements[id].children.map(card => card.workCode);
assert.deepStrictEqual(codes("colOverdueItems"), ["OVERDUE"]);
assert.deepStrictEqual(codes("colTodayItems"), ["TODAY"]);
assert.deepStrictEqual(codes("colUpcomingItems"), ["UPCOMING-START", "UPCOMING-END"]);
assert.deepStrictEqual(codes("colNoneItems"), ["NO-DUE"]);
const pairs = [
  ["countOverdue", "colOverdueItems", 1],
  ["countToday", "colTodayItems", 1],
  ["countUpcoming", "colUpcomingItems", 2],
  ["countNone", "colNoneItems", 1]
];
pairs.forEach(([counter, column, expected]) => {
  assert.strictEqual(Number(elements[counter].textContent), expected);
  assert.strictEqual(elements[column].children.length, expected);
});
const shown = ["colOverdueItems", "colTodayItems", "colUpcomingItems", "colNoneItems"].flatMap(codes);
assert.strictEqual(new Set(shown).size, shown.length);
assert.ok(!shown.includes("BEYOND-ONE"));
assert.ok(!shown.includes("BEYOND-FAR"));
assert.ok(!shown.includes("COMPLETED"));
assert.ok(!shown.includes("CANCELLED"));
assert.ok(!shown.includes("ARCHIVED"));
console.log("Deadline Tracker deterministic categorization: PASS");
`

	testPath := filepath.Join(t.TempDir(), "deadline_tracker_regression.js")
	if err := os.WriteFile(testPath, []byte(harness), 0600); err != nil {
		t.Fatalf("Gagal menulis harness Node sementara: %v", err)
	}
	cmd := exec.Command(nodePath, testPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Deadline Tracker regression gagal: %s (error: %v)", string(output), err)
	}
	t.Log(strings.TrimSpace(string(output)))
}

// TestBrandingAssetsServing memverifikasi bahwa aset favicon dan logo sidebar dilayani dengan HTTP 200
func TestBrandingAssetsServing(t *testing.T) {
	_, baseURL, cleanup := setupTestServer(t)
	defer cleanup()

	client := &http.Client{}

	// 1. Uji GET /favicon.ico (wajib HTTP 200 dan Content-Type image/x-icon)
	respFav, err := client.Get(baseURL + "/favicon.ico")
	if err != nil {
		t.Fatalf("Gagal mengakses /favicon.ico: %v", err)
	}
	defer respFav.Body.Close()

	if respFav.StatusCode != http.StatusOK {
		t.Errorf("Ekspektasi 200 OK untuk /favicon.ico, didapat: %d", respFav.StatusCode)
	}
	favContentType := respFav.Header.Get("Content-Type")
	if !strings.Contains(favContentType, "image/x-icon") && !strings.Contains(favContentType, "image/vnd.microsoft.icon") {
		t.Errorf("Content-Type /favicon.ico harus memuat icon mime, didapat: %s", favContentType)
	}
	favBytes, _ := io.ReadAll(respFav.Body)
	if len(favBytes) == 0 {
		t.Errorf("/favicon.ico tidak boleh kosong (0 byte)")
	}

	// 2. Uji GET /favicon.svg
	respSvg, err := client.Get(baseURL + "/favicon.svg")
	if err != nil {
		t.Fatalf("Gagal mengakses /favicon.svg: %v", err)
	}
	defer respSvg.Body.Close()
	if respSvg.StatusCode != http.StatusOK {
		t.Errorf("Ekspektasi 200 OK untuk /favicon.svg, didapat: %d", respSvg.StatusCode)
	}

	// 3. Uji GET /assets/ComplianceWorkspace_logo_full_horizontal_white.png (Sidebar Logo)
	respLogo, err := client.Get(baseURL + "/assets/ComplianceWorkspace_logo_full_horizontal_white.png")
	if err != nil {
		t.Fatalf("Gagal mengakses logo sidebar: %v", err)
	}
	defer respLogo.Body.Close()
	if respLogo.StatusCode != http.StatusOK {
		t.Errorf("Ekspektasi 200 OK untuk logo sidebar, didapat: %d", respLogo.StatusCode)
	}
	logoBytes, _ := io.ReadAll(respLogo.Body)
	if len(logoBytes) == 0 {
		t.Errorf("Logo sidebar tidak boleh kosong (0 byte)")
	}

	// 4. Uji GET / (index.html memuat link favicon dan img logo sidebar)
	respIndex, err := client.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("Gagal mengakses index.html: %v", err)
	}
	defer respIndex.Body.Close()
	indexBytes, _ := io.ReadAll(respIndex.Body)
	indexStr := string(indexBytes)

	if !strings.Contains(indexStr, `rel="icon" href="/favicon.ico"`) {
		t.Errorf("index.html harus memuat link rel=\"icon\" href=\"/favicon.ico\"")
	}
	if !strings.Contains(indexStr, "ComplianceWorkspace_logo_full_horizontal_white.png") {
		t.Errorf("index.html harus memuat tag img logo sidebar ComplianceWorkspace_logo_full_horizontal_white.png")
	}
}
