package service

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"

	"compliance-workspace/internal/domain"
	"github.com/xuri/excelize/v2"
)

// StandardHeaders mewakili nama kolom acuan standar Compliance Workspace
var StandardHeaders = []string{
	"title",
	"description",
	"category",
	"requested_by",
	"received_on",
	"due_on",
	"priority",
	"status",
}

// ParsedFileData mewakili hasil parsing file mentah
type ParsedFileData struct {
	FileType   string
	Headers    []string
	Rows       []map[string]string
	Worksheets []string
	SheetName  string
}

// ParseImportFile memproses file CSV atau XLSX mentah menjadi baris terstruktur dengan batasan memori dan ukuran
func ParseImportFile(fileBytes []byte, fileName string, fileType string, sheetName string) (*ParsedFileData, error) {
	if len(fileBytes) == 0 {
		return nil, errors.New("berkas kosong (0 byte)")
	}
	if len(fileBytes) > domain.MaxImportFileSize {
		return nil, fmt.Errorf("ukuran berkas melebihi batas maksimal 10 MB (didapat: %.2f MB)", float64(len(fileBytes))/(1024*1024))
	}

	ft := strings.ToLower(fileType)
	if ft == "" {
		if strings.HasSuffix(strings.ToLower(fileName), ".csv") {
			ft = "csv"
		} else if strings.HasSuffix(strings.ToLower(fileName), ".xlsx") {
			ft = "xlsx"
		}
	}

	switch ft {
	case "csv":
		return parseCSVFile(fileBytes)
	case "xlsx":
		return parseXLSXFile(fileBytes, sheetName)
	default:
		return nil, fmt.Errorf("format berkas '%s' tidak didukung (hanya mendukung CSV dan XLSX)", ft)
	}
}

// parseCSVFile membaca file CSV dengan penanganan UTF-8 BOM dan sanitasi
func parseCSVFile(data []byte) (*ParsedFileData, error) {
	// Potong BOM UTF-8 (\xEF\xBB\xBF) jika ada di awal file
	cleanedData := data
	if len(cleanedData) >= 3 && cleanedData[0] == 0xEF && cleanedData[1] == 0xBB && cleanedData[2] == 0xBF {
		cleanedData = cleanedData[3:]
	}

	reader := csv.NewReader(bytes.NewReader(cleanedData))
	reader.TrimLeadingSpace = true
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1 // izinkan panjang baris fleksibel, divalidasi per header

	rawHeaders, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return nil, errors.New("berkas CSV tidak memiliki baris judul kolom")
		}
		return nil, fmt.Errorf("gagal membaca header CSV: %w", err)
	}

	var headers []string
	for _, h := range rawHeaders {
		trimmed := strings.TrimSpace(h)
		if trimmed != "" {
			headers = append(headers, trimmed)
		}
	}
	if len(headers) == 0 {
		return nil, errors.New("tidak ada judul kolom yang valid dalam berkas CSV")
	}

	var rows []map[string]string
	rowCount := 0

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("baris CSV %d rusak atau tidak terbaca: %w", rowCount+2, err)
		}

		// Periksa apakah baris sepenuhnya kosong
		isEmpty := true
		for _, val := range record {
			if strings.TrimSpace(val) != "" {
				isEmpty = false
				break
			}
		}
		if isEmpty {
			continue
		}

		rowCount++
		if rowCount > domain.MaxImportRows {
			return nil, fmt.Errorf("jumlah baris melebihi batas maksimal 5.000 baris per batch")
		}

		rowMap := make(map[string]string)
		for i, h := range headers {
			if i < len(record) {
				rowMap[h] = strings.TrimSpace(record[i])
			} else {
				rowMap[h] = ""
			}
		}
		rows = append(rows, rowMap)
	}

	return &ParsedFileData{
		FileType: "csv",
		Headers:  headers,
		Rows:     rows,
	}, nil
}

// validateXLSXArchive memeriksa integritas arsip ZIP XLSX dan membatasi ukuran terdekompresi serta rasio kompresi (anti zip bomb / resource exhaustion)
func validateXLSXArchive(data []byte) error {
	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("gagal membuka arsip XLSX: format bukan ZIP yang valid atau terenkripsi: %w", err)
	}

	var totalUncompressed uint64
	for _, f := range zipReader.File {
		if strings.Contains(f.Name, "..") {
			return errors.New("arsip XLSX mengandung jalur berkas yang tidak aman")
		}
		totalUncompressed += f.UncompressedSize64
		if totalUncompressed > domain.MaxXLSXUncompressedSize {
			return fmt.Errorf("ukuran konten XLSX setelah dekompresi (%.2f MB) melebihi batas aman %d MB (potensi pemborosan sumber daya)",
				float64(totalUncompressed)/(1024*1024), domain.MaxXLSXUncompressedSize/(1024*1024))
		}
	}

	if len(data) > 0 {
		ratio := float64(totalUncompressed) / float64(len(data))
		if ratio > float64(domain.MaxXLSXCompressionRatio) && totalUncompressed > 5*1024*1024 {
			return fmt.Errorf("rasio kompresi XLSX (%.1f:1) melebihi batas aman pemrosesan (maks %d:1)",
				ratio, domain.MaxXLSXCompressionRatio)
		}
	}

	return nil
}

// parseXLSXFile membaca file XLSX menggunakan excelize/v2 dengan pembatasan sel dan worksheet
func parseXLSXFile(data []byte, requestedSheet string) (*ParsedFileData, error) {
	// Verifikasi pre-flight struktur arsip zip dan batas ukuran terdekompresi
	if err := validateXLSXArchive(data); err != nil {
		return nil, err
	}

	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gagal membuka berkas Excel: format rusak atau terenkripsi: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, errors.New("berkas Excel tidak memiliki lembar kerja")
	}
	if len(sheets) > domain.MaxXLSXWorksheets {
		return nil, fmt.Errorf("jumlah lembar kerja (%d) melebihi batas maksimal %d lembar", len(sheets), domain.MaxXLSXWorksheets)
	}

	targetSheet := sheets[0]
	if requestedSheet != "" {
		found := false
		for _, s := range sheets {
			if strings.EqualFold(s, requestedSheet) {
				targetSheet = s
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("lembar kerja '%s' tidak ditemukan dalam berkas Excel", requestedSheet)
		}
	}

	rowsIter, err := f.Rows(targetSheet)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca lembar kerja '%s': %w", targetSheet, err)
	}
	defer rowsIter.Close()

	if !rowsIter.Next() {
		return nil, fmt.Errorf("lembar kerja '%s' kosong", targetSheet)
	}

	rawHeaders, err := rowsIter.Columns()
	if err != nil {
		return nil, fmt.Errorf("gagal membaca header Excel: %w", err)
	}

	var headers []string
	for _, h := range rawHeaders {
		trimmed := strings.TrimSpace(h)
		if trimmed != "" {
			headers = append(headers, trimmed)
		}
	}
	if len(headers) == 0 {
		return nil, errors.New("tidak ada nama kolom header valid pada baris pertama Excel")
	}

	var rows []map[string]string
	rowCount := 0
	totalCells := len(headers)

	for rowsIter.Next() {
		record, err := rowsIter.Columns()
		if err != nil {
			return nil, fmt.Errorf("baris Excel %d tidak dapat dibaca: %w", rowCount+2, err)
		}

		isEmpty := true
		for _, val := range record {
			if strings.TrimSpace(val) != "" {
				isEmpty = false
				break
			}
		}
		if isEmpty {
			continue
		}

		rowCount++
		if rowCount > domain.MaxImportRows {
			return nil, fmt.Errorf("jumlah baris Excel melebihi batas maksimal 5.000 baris")
		}

		totalCells += len(record)
		if totalCells > domain.MaxXLSXCells {
			return nil, fmt.Errorf("total sel Excel melebihi batas aman pemrosesan (maks 100.000 sel)")
		}

		rowMap := make(map[string]string)
		for i, h := range headers {
			if i < len(record) {
				rowMap[h] = strings.TrimSpace(record[i])
			} else {
				rowMap[h] = ""
			}
		}
		rows = append(rows, rowMap)
	}

	return &ParsedFileData{
		FileType:   "xlsx",
		Headers:    headers,
		Rows:       rows,
		Worksheets: sheets,
		SheetName:  targetSheet,
	}, nil
}

// isActivityLogFile mendeteksi apakah file yang diupload adalah ekspor Activity Log
// berdasarkan kehadiran header yang khas dari tabel activity_logs.
// Digunakan untuk menolak file yang salah sebelum mapping dilakukan.
func isActivityLogFile(headers []string) bool {
	activityLogSignatures := []string{
		"activity_type", "activity type",
		"occurred_at", "occurred at",
		"recorded_at", "recorded at",
		"field_name", "field name",
		"old_value", "old value",
		"new_value", "new value",
		"jenis aktivitas", "jenis kegiatan",
		"waktu kejadian", "waktu pencatatan",
	}
	matchCount := 0
	for _, h := range headers {
		normalized := strings.ToLower(strings.TrimSpace(h))
		for _, sig := range activityLogSignatures {
			if normalized == sig || strings.Contains(normalized, sig) {
				matchCount++
				break
			}
		}
		if matchCount >= 2 {
			return true
		}
	}
	return false
}

// SuggestColumnMapping menganalisis header file dan mencocokkannya secara cerdas dengan kolom baku
func SuggestColumnMapping(detectedHeaders []string) map[string]string {
	mapping := make(map[string]string)

	synonyms := map[string][]string{
		"title":        {"judul", "title", "nama pekerjaan", "nama tugas", "pekerjaan", "subjek", "subject", "kegiatan"},
		"description":  {"deskripsi", "description", "uraian", "keterangan", "ruang lingkup", "detail", "catatan"},
		"category":     {"kategori", "category", "jenis", "tipe", "klasifikasi"},
		"requested_by": {"pemberi tugas", "requested_by", "pemohon", "asal permintaan", "instansi", "requester", "penugasan oleh"},
		"received_on":  {"tanggal diterima", "received_on", "tgl diterima", "diterima", "tanggal masuk", "tgl masuk", "tanggal"},
		"due_on":       {"tenggat", "due_on", "tenggat waktu", "batas waktu", "jatuh tempo", "deadline", "tgl tenggat"},
		"priority":     {"prioritas", "priority", "tingkat prioritas", "urgensi"},
		"status":       {"status", "status pekerjaan", "kondisi", "state"},
	}

	for targetField, terms := range synonyms {
		var exactMatches []string
		for _, h := range detectedHeaders {
			normalized := strings.ToLower(strings.TrimSpace(h))
			for _, term := range terms {
				if normalized == term {
					exactMatches = append(exactMatches, h)
					break
				}
			}
		}
		if len(exactMatches) == 1 {
			mapping[targetField] = exactMatches[0]
			continue
		}
		if len(exactMatches) > 1 {
			continue
		}

		var partialMatches []string
		for _, h := range detectedHeaders {
			normalized := strings.ToLower(strings.TrimSpace(h))
			for _, term := range terms {
				// "pekerjaan" dan "kegiatan" sah sebagai header title mandiri,
				// tetapi terlalu generik untuk substring seperti "Kode Pekerjaan".
				if targetField == "title" && (term == "pekerjaan" || term == "kegiatan") {
					continue
				}
				if strings.Contains(normalized, term) {
					partialMatches = append(partialMatches, h)
					break
				}
			}
		}
		if len(partialMatches) == 1 {
			mapping[targetField] = partialMatches[0]
		}
	}

	return mapping
}

// GenerateCSVTemplate menghasilkan data byte template CSV ber-BOM UTF-8
func GenerateCSVTemplate() []byte {
	var buf bytes.Buffer
	// Tulis UTF-8 BOM agar langsung rapi di Microsoft Excel
	buf.Write([]byte{0xEF, 0xBB, 0xBF})

	writer := csv.NewWriter(&buf)
	_ = writer.Write([]string{
		"Judul Pekerjaan",
		"Deskripsi",
		"Kategori",
		"Pemberi Tugas",
		"Tanggal Diterima",
		"Tenggat Waktu",
		"Prioritas",
		"Status",
	})

	_ = writer.Write([]string{
		"Tinjauan Kepatuhan Perlindungan Data Pribadi (UU PDP)",
		"Review kesiapan kepatuhan pemrosesan data pribadi nasabah dan pemetaan data flow",
		"Kepatuhan Internal",
		"Direksi",
		"2026-10-17",
		"2026-11-15",
		"high",
		"not_started",
	})

	_ = writer.Write([]string{
		"Penyusunan Laporan Triwulan Penerapan APU-PPT",
		"Kompilasi transaksi keuangan mencurigakan (LTKM) dan laporan berkala ke PPATK",
		"Pelaporan Regulator",
		"Komite Kepatuhan",
		"2026-10-17",
		"2026-10-31",
		"normal",
		"in_progress",
	})

	writer.Flush()
	return buf.Bytes()
}

// GenerateXLSXTemplate menghasilkan file spreadsheet Excel berformat baku dengan styling profesional
func GenerateXLSXTemplate() ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	sheet := "Work Register Template"
	f.SetSheetName("Sheet1", sheet)

	headers := []string{
		"Judul Pekerjaan",
		"Deskripsi",
		"Kategori",
		"Pemberi Tugas",
		"Tanggal Diterima",
		"Tenggat Waktu",
		"Prioritas",
		"Status",
	}

	// Buat style header: biru tua, teks putih tebal, rata tengah
	headerStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:  true,
			Color: "#FFFFFF",
			Size:  11,
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"#1E3A8A"},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
			WrapText:   true,
		},
	})
	if err == nil {
		_ = f.SetRowStyle(sheet, 1, 1, headerStyle)
	}

	// Tulis Header
	for colIdx, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(colIdx+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}

	// Tulis Contoh Baris 1
	row1 := []interface{}{
		"Tinjauan Kepatuhan Perlindungan Data Pribadi (UU PDP)",
		"Review kesiapan pemrosesan data pribadi nasabah dan pemetaan data flow",
		"Kepatuhan Internal",
		"Direksi",
		"2026-10-17",
		"2026-11-15",
		"high",
		"not_started",
	}
	for colIdx, val := range row1 {
		cell, _ := excelize.CoordinatesToCellName(colIdx+1, 2)
		_ = f.SetCellValue(sheet, cell, val)
	}

	// Tulis Contoh Baris 2
	row2 := []interface{}{
		"Penyusunan Laporan Triwulan Penerapan APU-PPT",
		"Kompilasi transaksi keuangan mencurigakan (LTKM) dan laporan berkala ke PPATK",
		"Pelaporan Regulator",
		"Komite Kepatuhan",
		"2026-10-17",
		"2026-10-31",
		"normal",
		"in_progress",
	}
	for colIdx, val := range row2 {
		cell, _ := excelize.CoordinatesToCellName(colIdx+1, 3)
		_ = f.SetCellValue(sheet, cell, val)
	}

	// Atur lebar kolom yang representatif
	widths := map[string]float64{
		"A": 36, // Judul
		"B": 48, // Deskripsi
		"C": 22, // Kategori
		"D": 20, // Pemberi Tugas
		"E": 18, // Tanggal Diterima
		"F": 18, // Tenggat
		"G": 14, // Prioritas
		"H": 16, // Status
	}
	for col, width := range widths {
		_ = f.SetColWidth(sheet, col, col, width)
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("gagal merender template XLSX: %w", err)
	}
	return buf.Bytes(), nil
}
