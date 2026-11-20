package models

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/huggingface"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shirou/gopsutil/v3/mem"
)

type filterCategory int

const (
	filterAll filterCategory = iota
	filterLocal
	filterDownloaded
	filterCloud
	filterHuggingFace
	filterCount
)

func (f filterCategory) label() string {
	switch f {
	case filterLocal:
		return "Local"
	case filterDownloaded:
		return "Downloaded"
	case filterCloud:
		return "Cloud"
	case filterHuggingFace:
		return "Hugging Face"
	default:
		return "All"
	}
}

type hfSearchResultMsg struct {
	query  string
	models []ModelInfo
	err    error
}

type hfDownloadResultMsg struct {
	repoID    string
	file      huggingface.GGUFFile
	localPath string
	modelInfo ModelInfo
	err       error
}

type model struct {
	catalog        []ModelInfo
	cursor         int
	selected       ModelInfo
	activeID       string
	err            error
	ctx            context.Context
	hfClient       *huggingface.Client
	localModelsDir string
	availableRAMGB float64
	filterTab      filterCategory
	searchMode     bool
	searchQuery    string
	hfInputMode    bool
	hfQuery        string
	hfModels       []ModelInfo
	hfLoading      bool
	statusMsg      string
	scrollOffset   int
	width          int
	height         int
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) contextOrBackground() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

func (m model) getHFClient() *huggingface.Client {
	if m.hfClient != nil {
		return m.hfClient
	}
	return huggingface.NewClient("")
}

func (m model) getLocalModelsDir() string {
	if strings.TrimSpace(m.localModelsDir) != "" {
		return m.localModelsDir
	}
	return DefaultLocalModelsDir()
}

func (m model) getAvailableRAMGB() float64 {
	if m.availableRAMGB > 0 {
		return m.availableRAMGB
	}
	if v, err := mem.VirtualMemoryWithContext(m.contextOrBackground()); err == nil && v != nil {
		if v.Available > 0 {
			return float64(v.Available) / (1024 * 1024 * 1024)
		}
		if v.Total > 0 {
			return float64(v.Total) / (1024 * 1024 * 1024) * 0.75
		}
	}
	return 0
}

func (m model) visibleModels() []ModelInfo {
	var base []ModelInfo
	if m.filterTab == filterHuggingFace {
		if len(m.hfModels) > 0 {
			base = m.hfModels
		} else {
			for _, item := range m.catalog {
				if item.Provider == "huggingface" || strings.HasPrefix(strings.ToLower(item.ID), "hf.co/") {
					base = append(base, item)
				}
			}
		}
	} else {
		for _, item := range m.catalog {
			switch m.filterTab {
			case filterAll:
				base = append(base, item)
			case filterLocal:
				if item.Offline {
					base = append(base, item)
				}
			case filterDownloaded:
				if strings.Contains(strings.ToLower(item.Status), "downloaded") {
					base = append(base, item)
				}
			case filterCloud:
				if item.Provider == "cloud" {
					base = append(base, item)
				}
			}
		}
	}

	q := strings.ToLower(strings.TrimSpace(m.searchQuery))
	if q == "" {
		return base
	}

	filtered := make([]ModelInfo, 0, len(base))
	for _, item := range base {
		if strings.Contains(strings.ToLower(item.ID), q) ||
			strings.Contains(strings.ToLower(item.Name), q) ||
			strings.Contains(strings.ToLower(item.Category), q) ||
			strings.Contains(strings.ToLower(item.Description), q) ||
			strings.Contains(strings.ToLower(item.Provider), q) ||
			strings.Contains(strings.ToLower(item.Status), q) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (m model) pageSize() int {
	if m.height <= 0 {
		return 6
	}
	// Reserve ~15 lines for header, tab bar, search/status bar, category headers, borders, and footer.
	availLines := m.height - 15
	if availLines < 6 {
		return 3
	}
	slots := availLines / 3
	if slots < 3 {
		slots = 3
	}
	if slots > 8 {
		slots = 8
	}
	return slots
}

func (m *model) clampCursorAndScroll(total int) {
	if total <= 0 {
		m.cursor = 0
		m.scrollOffset = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	} else if m.cursor >= total {
		m.cursor = total - 1
	}

	page := m.pageSize()
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+page {
		m.scrollOffset = m.cursor - page + 1
	}
	maxOffset := total - page
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

func (m model) searchHFCmd(query string) tea.Cmd {
	ctx := m.contextOrBackground()
	client := m.getHFClient()
	return func() tea.Msg {
		results, err := client.SearchModels(ctx, huggingface.SearchOptions{
			Query:    query,
			GGUFOnly: true,
			Sort:     "downloads",
			Limit:    15,
		})
		if err != nil {
			return hfSearchResultMsg{query: query, err: err}
		}

		items := make([]ModelInfo, 0, len(results))
		for _, r := range results {
			items = append(items, ModelInfo{
				ID:            r.ID,
				Name:          r.ID,
				Provider:      "huggingface",
				Tier:          "HF",
				Category:      "Hugging Face Hub (GGUF)",
				Description:   fmt.Sprintf("Downloads: %d  |  Likes: %d", r.Downloads, r.Likes),
				Offline:       true,
				Status:        "Remote GGUF (Press 'd' to auto-select & download)",
				ContextWindow: safeContextWindow(r.ID),
				Downloads:     r.Downloads,
				Likes:         r.Likes,
			})
		}
		return hfSearchResultMsg{query: query, models: items}
	}
}

func (m model) autoDownloadHFCmd(repoID string) tea.Cmd {
	ctx := m.contextOrBackground()
	client := m.getHFClient()
	destDir := m.getLocalModelsDir()
	availRAM := m.getAvailableRAMGB()

	return func() tea.Msg {
		cleanRepo, _, _ := huggingface.ParseRef(repoID)
		files, err := client.ListGGUFFiles(ctx, cleanRepo)
		if err != nil {
			return hfDownloadResultMsg{repoID: cleanRepo, err: err}
		}
		best, err := huggingface.SelectBestGGUF(files, availRAM, "")
		if err != nil {
			return hfDownloadResultMsg{repoID: cleanRepo, err: err}
		}
		if strings.TrimSpace(destDir) == "" {
			return hfDownloadResultMsg{repoID: cleanRepo, err: fmt.Errorf("local models directory is unavailable")}
		}
		localPath, err := client.DownloadGGUF(ctx, cleanRepo, best.Filename, destDir, nil)
		if err != nil {
			return hfDownloadResultMsg{repoID: cleanRepo, err: err}
		}

		relID := filepath.Base(localPath)
		if rel, relErr := filepath.Rel(destDir, localPath); relErr == nil {
			relID = filepath.ToSlash(rel)
		}

		info := ModelInfo{
			ID:            relID,
			Name:          filepath.Base(localPath),
			Provider:      "gguf",
			Tier:          "LOCAL",
			Category:      customModelsCategory,
			Description:   fmt.Sprintf("Downloaded from %s (%s, %s)", cleanRepo, best.Quantization, formatBytes(best.SizeBytes)),
			Offline:       true,
			Status:        fmt.Sprintf("Downloaded GGUF (%s)", formatBytes(best.SizeBytes)),
			MinRAMGB:      best.EstimatedRAMGB,
			ContextWindow: safeContextWindow(best.Filename),
			SizeBytes:     best.SizeBytes,
			Quantization:  best.Quantization,
			LocalPath:     localPath,
		}
		return hfDownloadResultMsg{
			repoID:    cleanRepo,
			file:      best,
			localPath: localPath,
			modelInfo: info,
		}
	}
}

func trimLastRune(s string) string {
	if s == "" {
		return ""
	}
	_, size := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-size]
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.clampCursorAndScroll(len(m.visibleModels()))
		return m, nil

	case hfSearchResultMsg:
		m.hfLoading = false
		if msg.err != nil {
			m.err = msg.err
			m.statusMsg = fmt.Sprintf("Hugging Face search error: %v", msg.err)
			return m, nil
		}
		m.err = nil
		m.hfModels = msg.models
		m.filterTab = filterHuggingFace
		m.cursor = 0
		m.scrollOffset = 0
		if len(msg.models) == 0 {
			m.statusMsg = fmt.Sprintf("No GGUF models found on Hugging Face for %q.", msg.query)
		} else {
			m.statusMsg = fmt.Sprintf("Found %d Hugging Face GGUF repo(s) for %q. Press 'd' to download best RAM-fitting GGUF.", len(msg.models), msg.query)
		}
		return m, nil

	case hfDownloadResultMsg:
		m.hfLoading = false
		if msg.err != nil {
			m.err = msg.err
			m.statusMsg = fmt.Sprintf("Download failed: %v", msg.err)
			return m, nil
		}
		m.err = nil
		// Update or append in catalog
		replaced := false
		for i := range m.catalog {
			if m.catalog[i].ID == msg.modelInfo.ID {
				m.catalog[i] = msg.modelInfo
				replaced = true
				break
			}
		}
		if !replaced {
			m.catalog = append(m.catalog, msg.modelInfo)
		}
		// Update matching HF search result display
		for i := range m.hfModels {
			if m.hfModels[i].ID == msg.repoID {
				m.hfModels[i].Status = fmt.Sprintf("Downloaded %s -> %s", msg.file.Quantization, msg.modelInfo.ID)
				m.hfModels[i].LocalPath = msg.localPath
			}
		}
		if err := config.SetActiveModel(msg.modelInfo.ID); err != nil {
			m.err = err
		} else {
			m.activeID = msg.modelInfo.ID
			m.selected = msg.modelInfo
		}
		m.statusMsg = fmt.Sprintf("Downloaded %s (%s) and set active: %s", msg.file.Filename, msg.file.Quantization, msg.modelInfo.ID)
		return m, nil

	case tea.KeyMsg:
		if m.hfInputMode {
			return m.handleHFInputKey(msg)
		}
		if m.searchMode {
			return m.handleSearchInputKey(msg)
		}
		return m.handleNormalKey(msg)
	}
	return m, nil
}

func (m model) handleHFInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.hfInputMode = false
		return m, nil
	case tea.KeyBackspace:
		m.hfQuery = trimLastRune(m.hfQuery)
		return m, nil
	case tea.KeyEnter:
		q := strings.TrimSpace(m.hfQuery)
		m.hfInputMode = false
		if q == "" {
			return m, nil
		}
		m.hfLoading = true
		m.err = nil
		m.statusMsg = fmt.Sprintf("Searching Hugging Face for %q...", q)
		return m, m.searchHFCmd(q)
	case tea.KeySpace:
		m.hfQuery += " "
		return m, nil
	case tea.KeyRunes:
		m.hfQuery += string(msg.Runes)
		return m, nil
	}
	return m, nil
}

func (m model) handleSearchInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.searchMode = false
		m.searchQuery = ""
		m.cursor = 0
		m.scrollOffset = 0
		return m, nil
	case tea.KeyEnter:
		m.searchMode = false
		return m, nil
	case tea.KeyBackspace:
		m.searchQuery = trimLastRune(m.searchQuery)
		m.cursor = 0
		m.scrollOffset = 0
		return m, nil
	case tea.KeyTab:
		m.searchMode = false
		m.filterTab = (m.filterTab + 1) % filterCount
		m.cursor = 0
		m.scrollOffset = 0
		return m, nil
	case tea.KeyUp:
		return m.moveCursor(-1), nil
	case tea.KeyDown:
		return m.moveCursor(1), nil
	case tea.KeySpace:
		m.searchQuery += " "
		m.cursor = 0
		m.scrollOffset = 0
		return m, nil
	case tea.KeyRunes:
		m.searchQuery += string(msg.Runes)
		m.cursor = 0
		m.scrollOffset = 0
		return m, nil
	}
	return m, nil
}

func (m model) moveCursor(delta int) model {
	visible := m.visibleModels()
	if len(visible) == 0 {
		m.cursor = 0
		m.scrollOffset = 0
		return m
	}
	if delta == -1 && m.cursor == 0 {
		m.cursor = len(visible) - 1
	} else if delta == 1 && m.cursor == len(visible)-1 {
		m.cursor = 0
	} else {
		m.cursor += delta
	}
	m.clampCursorAndScroll(len(visible))
	return m
}

func (m model) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		if m.searchQuery != "" {
			m.searchQuery = ""
			m.cursor = 0
			m.scrollOffset = 0
			return m, nil
		}
		return m, tea.Quit
	case "tab":
		m.filterTab = (m.filterTab + 1) % filterCount
		m.cursor = 0
		m.scrollOffset = 0
		return m, nil
	case "shift+tab":
		m.filterTab = (m.filterTab - 1 + filterCount) % filterCount
		m.cursor = 0
		m.scrollOffset = 0
		return m, nil
	case "/":
		m.searchMode = true
		return m, nil
	case "h":
		m.filterTab = filterHuggingFace
		m.hfInputMode = true
		m.cursor = 0
		m.scrollOffset = 0
		return m, nil
	case "up", "k":
		return m.moveCursor(-1), nil
	case "down", "j":
		return m.moveCursor(1), nil
	case "pgup":
		visible := m.visibleModels()
		if len(visible) == 0 {
			return m, nil
		}
		m.cursor -= m.pageSize()
		m.clampCursorAndScroll(len(visible))
		return m, nil
	case "pgdown":
		visible := m.visibleModels()
		if len(visible) == 0 {
			return m, nil
		}
		m.cursor += m.pageSize()
		m.clampCursorAndScroll(len(visible))
		return m, nil
	case "home":
		m.cursor = 0
		m.scrollOffset = 0
		return m, nil
	case "end":
		visible := m.visibleModels()
		if len(visible) > 0 {
			m.cursor = len(visible) - 1
			m.clampCursorAndScroll(len(visible))
		}
		return m, nil
	case "d", "a":
		if m.hfLoading {
			return m, nil
		}
		visible := m.visibleModels()
		if len(visible) > 0 && m.cursor >= 0 && m.cursor < len(visible) {
			item := visible[m.cursor]
			if item.Provider == "huggingface" {
				m.hfLoading = true
				m.err = nil
				m.statusMsg = fmt.Sprintf("Selecting best GGUF & downloading %s...", item.ID)
				return m, m.autoDownloadHFCmd(item.ID)
			}
		}
		return m, nil
	case "enter":
		visible := m.visibleModels()
		if len(visible) > 0 && m.cursor >= 0 && m.cursor < len(visible) {
			picked := visible[m.cursor]
			if picked.Provider == "huggingface" && picked.LocalPath == "" {
				picked.ID = huggingface.OllamaRef(picked.ID, "")
			}
			m.selected = picked
			if err := config.SetActiveModel(m.selected.ID); err != nil {
				m.err = err
			} else {
				m.activeID = m.selected.ID
			}
		}
		return m, tea.Quit
	}
	return m, nil
}

func formatModelStatus(status string) string {
	lower := strings.ToLower(status)
	switch {
	case strings.Contains(lower, "downloaded"):
		return lipgloss.NewStyle().Foreground(lipgloss.Color("48")).Render(status)
	case strings.Contains(lower, "available cloud"):
		return lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Render(status)
	case strings.Contains(lower, "requires"):
		return lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(status)
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(status)
	}
}

func (m model) View() string {
	if len(m.catalog) == 0 && len(m.hfModels) == 0 && m.filterTab != filterHuggingFace && !m.hfInputMode {
		return "No models available in catalog.\nPress 'q' to quit.\n"
	}

	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true).Render("🤖  AGY MODEL SELECTOR & HARDWARE AUDIT") + "\n")

	// Render provider/category filter tabs
	var tabs []string
	activeTabStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("99")).Bold(true).Padding(0, 1)
	inactiveTabStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Padding(0, 1)
	for i := filterCategory(0); i < filterCount; i++ {
		if i == m.filterTab {
			tabs = append(tabs, activeTabStyle.Render(i.label()))
		} else {
			tabs = append(tabs, inactiveTabStyle.Render(i.label()))
		}
	}
	sb.WriteString(strings.Join(tabs, " ") + "\n")

	// Search / HF input bar
	if m.hfInputMode {
		prompt := lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true).Render("HF Search > ")
		sb.WriteString(prompt + m.hfQuery + "█  " + lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("(Enter to search, Esc to cancel)") + "\n")
	} else if m.searchMode {
		prompt := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true).Render("Filter / > ")
		sb.WriteString(prompt + m.searchQuery + "█  " + lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("(Enter to apply, Esc to clear)") + "\n")
	} else if m.searchQuery != "" {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Render(fmt.Sprintf("Filter: %q  (Esc to clear)", m.searchQuery)) + "\n")
	}

	if m.statusMsg != "" {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(m.statusMsg) + "\n")
	}
	sb.WriteString("\n")

	visible := m.visibleModels()
	selectedIDStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true)
	selectedDescStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	normalIDStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Bold(true)
	normalDescStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	activeBadgeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("48")).Bold(true).Padding(0, 1)

	if len(visible) == 0 {
		if m.filterTab == filterHuggingFace {
			sb.WriteString("  No Hugging Face search results yet. Press 'h' to search Hugging Face Hub for GGUF models.\n")
		} else {
			sb.WriteString("  No models match the active filter/search.\n")
		}
	} else {
		page := m.pageSize()
		start := m.scrollOffset
		if m.cursor < start {
			start = m.cursor
		}
		if m.cursor >= start+page {
			start = m.cursor - page + 1
		}
		if start < 0 {
			start = 0
		}
		if start >= len(visible) {
			start = 0
		}
		end := start + page
		if end > len(visible) {
			end = len(visible)
		}

		if start > 0 {
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render(fmt.Sprintf("  ▲ %d more above", start)) + "\n")
		}

		var currentCategory string
		for i := start; i < end; i++ {
			item := visible[i]
			if item.Category != currentCategory && item.Category != "" {
				if i > start {
					sb.WriteString("\n")
				}
				currentCategory = item.Category
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true).Render("  ━━━━ "+currentCategory+" ━━━━") + "\n")
			}

			cursor := "  "
			idStyled := normalIDStyle.Render(fmt.Sprintf("%-26s", item.ID))
			descStyled := normalDescStyle.Render(item.Description)
			if m.cursor == i {
				cursor = selectedIDStyle.Render("❯ ")
				idStyled = selectedIDStyle.Render(fmt.Sprintf("%-26s", item.ID))
				descStyled = selectedDescStyle.Render(item.Description)
			}

			activeTag := ""
			if item.ID == m.activeID && m.activeID != "" {
				activeTag = "  " + activeBadgeStyle.Render("ACTIVE MODEL")
			}

			sb.WriteString(fmt.Sprintf("%s%s %s  %s%s\n    %s\n",
				cursor, idStyled, item.RenderBadge(), formatModelStatus(item.Status), activeTag, descStyled))
		}

		if end < len(visible) {
			sb.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render(fmt.Sprintf("  ▼ %d more below (showing %d-%d of %d)", len(visible)-end, start+1, end, len(visible))) + "\n")
		} else if len(visible) > page {
			sb.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render(fmt.Sprintf("  Showing %d-%d of %d", start+1, end, len(visible))) + "\n")
		}
	}

	sb.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("147")).Render("[↑/↓/PgUp/PgDn] Navigate • [Tab] Filter • [/] Search • [h] HF • [d] Download • [Enter] Select • [q] Quit"))

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("99")).
		Padding(1, 3)

	return boxStyle.Render(sb.String())
}

// RunTUI launches the interactive model selector using default scanner and Hugging Face client.
func RunTUI(ctx context.Context) (ModelInfo, bool) {
	return RunTUIWithDeps(ctx, NewScanner(), huggingface.NewClient(""))
}

// RunTUIWithDeps launches the interactive model selector with injectable scanner and Hugging Face client.
func RunTUIWithDeps(ctx context.Context, scanner *Scanner, hfClient *huggingface.Client) (ModelInfo, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ModelInfo{}, false
	}
	if scanner == nil {
		scanner = NewScanner()
	}
	catalog, err := scanner.Scan(ctx)
	if err != nil || len(catalog) == 0 {
		return ModelInfo{}, false
	}

	activeID := config.GetActiveModel()

	// Initial cursor position at active model
	initialCursor := 0
	for i, m := range catalog {
		if m.ID == activeID {
			initialCursor = i
			catalog[i].IsActive = true
			break
		}
	}

	var availRAMGB float64
	if v, memErr := mem.VirtualMemoryWithContext(ctx); memErr == nil && v != nil {
		if v.Available > 0 {
			availRAMGB = float64(v.Available) / (1024 * 1024 * 1024)
		} else if v.Total > 0 {
			availRAMGB = float64(v.Total) / (1024 * 1024 * 1024) * 0.75
		}
	}

	initialModel := model{
		catalog:        catalog,
		cursor:         initialCursor,
		activeID:       activeID,
		ctx:            ctx,
		hfClient:       hfClient,
		localModelsDir: scanner.resolveLocalModelsDir(),
		availableRAMGB: availRAMGB,
	}
	initialModel.clampCursorAndScroll(len(catalog))

	p := tea.NewProgram(initialModel, tea.WithContext(ctx), tea.WithAltScreen())

	m, err := p.Run()
	if err != nil {
		return ModelInfo{}, false
	}

	if finalModel, ok := m.(model); ok && finalModel.selected.ID != "" {
		if finalModel.err != nil {
			fmt.Fprintf(os.Stderr, "failed to save active model %s: %v\n", finalModel.selected.ID, finalModel.err)
			return ModelInfo{}, false
		}
		return finalModel.selected, true
	}
	return ModelInfo{}, false
}
