# 全平台自動化建置架構規格 (Cross-Platform Architecture Spec)

本文件詳述了如何透過 Go 語言、資源嵌入技術與 CI/CD 工具，達成「單一程式碼庫，全平台自動產出執行檔」的架構方案。此方案特別適用於具備 Web UI 介面的桌面應用程式或工具（如 XBVR）。

---

## 1. 核心開發語言：Go (Golang)
Go 語言原生支援交叉編譯（Cross-compilation），開發者不需在目標作業系統上即可產出該平台的二進制檔案。

### 關鍵機制：
*   **環境變數驅動**：透過設定 `GOOS`（作業系統）與 `GOARCH`（硬體架構）來指派編譯目標。
    *   `windows`, `linux`, `darwin` (macOS)
    *   `amd64` (Intel/AMD), `arm64` (Apple M1/M2/M3, 現代手機/伺服器), `arm` (樹莓派等)
*   **指令範例**：
    ```bash
    # 產出 Windows 64-bit 執行檔
    GOOS=windows GOARCH=amd64 go build -o myapp.exe
    ```

---

## 2. 前端資源嵌入方案 (Asset Embedding)
為了達成「單一執行檔即開即用」，系統將 Web 前端產出的靜態資源（HTML, JS, CSS）直接打包進 Go 的二進制檔案中。

### 實作步驟：
1.  **前端構建**：先執行前端框架的打包指令（如 `npm run build`），產出 `dist` 資料夾。
2.  **Go Embed 指令**：使用 Go 1.16+ 引入的 `//go:embed` 指令。
    ```go
    // pkg/ui/ui.go
    package ui

    import "embed"

    //go:embed dist/*
    var frontendContent embed.FS

    func GetFileSystem() http.FileSystem {
        return http.FS(frontendContent)
    }
    ```
3.  **內建 Web Server**：程式啟動後，直接從記憶體（而非硬碟路徑）讀取這些網頁資源並架設服務。

---

## 3. 自動化建置生產線：GoReleaser + GitHub Actions
解決跨平台編譯中最難處理的 「C 語言依賴 (CGO)」與「多平台打包」問題。

### 關鍵工具：
*   **GoReleaser**：專為 Go 設計的發布工具，透過 `.goreleaser.yml` 配置所有建置規則。
*   **goreleaser-cross**：這是一個專門的 Docker 鏡像，預裝了各平台的 C 交叉編譯器。當專案需要 CGO（如使用 SQLite）時，這是達成全平台編譯的唯一解法。

### 自動化工作流 (Workflow)：
1.  **觸發點**：開發者推送一個新的 Git Tag（如 `v1.0.0`）。
2.  **CI 運行 (GitHub Actions)**：
    *   **Step 1**: 簽出代碼。
    *   **Step 2**: 執行前端構建 (`yarn install && yarn build`)。
    *   **Step 3**: 啟動 GoReleaser。
    *   **Step 4**: GoReleaser 調用 Docker 進行多平台 CGO 編譯、產出餘數校驗碼 (Checksums)、產生變更日誌 (Changelog)。
    *   **Step 5**: 自動建立 GitHub Release 並上傳所有壓縮包。

---

## 4. GitHub 環境設置指南 (Setup Guide)

要讓上述自動化流程跑通，必須在 GitHub 進行以下安全設置：

### A. 產生個人存取權杖 (PAT - Classic)
1. **路徑**：`Personal Settings` -> `Developer settings` -> `Personal access tokens` -> `Tokens (classic)`。
2. **權限 (Scopes)**：務必勾選以下項目：
   - [x] **`repo`** (全選)：允許讀寫代碼、建立 Release。
   - [x] **`write:packages`**：允許將 Docker 鏡像推送到 GitHub Container Registry (GHCR)。
3. **備註**：產出後請立即複製 `ghp_` 開頭的字串，畫面關閉後將無法再查看。

### B. 設定專案祕鑰 (Actions Secrets)
1. **路徑**：進入您的 `Repo` -> `Settings` -> `Secrets and variables` -> `Actions`。
2. **新增 Secret**：
   - **Name**: `GORELEASER_ACCESS_TOKEN`
   - **Value**: 貼上剛才產生的 `ghp_...` 字串。

---

## 5. 複製品評估清單 (Checklist for New Projects)

若您想在下一個專案套用此架構，請確保包含以下檔案：
*   [ ] **`go.mod`**：Go 專案核心。
*   [ ] **`ui/`**：前端專案目錄。
*   [ ] **`pkg/ui/ui.go`**：實作 `//go:embed` 嵌入邏輯。
*   [ ] **`.goreleaser.yml`**：定義編譯目標（Windows, Mac, Linux）。
*   [ ] **`.github/workflows/release.yml`**：定義 Action 觸發時機與 Token 權限。
*   [ ] **`Makefile`**：封裝 `goreleaser-cross` 的 Docker 執行指令。

---

## 5. 給 AI Agent 的整合指令
> 「請參考本文件的架構，為我現有的 Go 專案配置 GoReleaser 並實作前端資源嵌入。確保 CGO 支援已開啟，並能在推送到 Tag 時自動上傳不同作業系統的 Release Assets 到 GitHub。」
