package prompt

import (
	"embed"
	"strings"

	"github.com/kaanemec/things-items-inventory/internal/inventory"
)

// The prompt texts, one file per task plus shared rules, kept in the binary.
// When a task's text changes, bump its version below; a test fails until
// testdata/prompt_hashes.txt is updated too, so a changed prompt can't keep
// an old version. Schema changes need no bump: Version carries a schema hash.
//
//go:embed prompts/*.md
var promptFiles embed.FS

// Prompt versions, stored with every job result.
const (
	itemDraftVersion     = "item_draft.1"
	labelVersion         = "label.1"
	receiptVersion       = "receipt.1"
	contentsVersion      = "contents.1"
	rowCompletionVersion = "row_completion.1"
	textToItemsVersion   = "text_to_items.1"
	csvMappingVersion    = "csv_mapping.1"
)

// defaultCatalog is the category attribute sets of the inventory package.
var defaultCatalog = catalog(inventory.Categories)

// The seven tasks, built from the inventory's category attribute sets.
var (
	ItemDraft     = newItemDraft(defaultCatalog)
	Label         = newLabel(defaultCatalog)
	Receipt       = newReceipt(defaultCatalog)
	Contents      = newContents(defaultCatalog)
	RowCompletion = newRowCompletion(defaultCatalog)
	TextToItems   = newTextToItems(defaultCatalog)
	CSVMapping    = newCSVMapping(defaultCatalog)
)

// system composes a system prompt from prompt files.
func system(files ...string) string {
	parts := make([]string, 0, len(files))
	for _, f := range files {
		b, err := promptFiles.ReadFile("prompts/" + f + ".md")
		if err != nil {
			panic(err)
		}
		parts = append(parts, strings.TrimSpace(string(b)))
	}
	return strings.Join(parts, "\n\n")
}

func newItemDraft(c catalog) *Task[ItemDraftInput, ItemDraftAnswer] {
	return newTask("item_draft", itemDraftVersion, system("rules", "attributes", "item_draft"), 2000, c.enums(),
		itemDraftUser(c), checkItemDraft(c), checkItemDraftIn)
}

func newLabel(c catalog) *Task[LabelInput, LabelAnswer] {
	return newTask[LabelInput, LabelAnswer]("label", labelVersion, system("rules", "attributes", "label"), 1500, c.enums(),
		labelUser(c), checkLabel(c), nil)
}

func newReceipt(c catalog) *Task[ReceiptInput, ReceiptAnswer] {
	return newTask[ReceiptInput, ReceiptAnswer]("receipt", receiptVersion, system("rules", "receipt"), 3000, c.enums(),
		receiptUser(c), checkReceipt, nil)
}

func newContents(c catalog) *Task[ContentsInput, ContentsAnswer] {
	return newTask("contents", contentsVersion, system("rules", "contents"), 3000, c.enums(),
		contentsUser(c), checkContents, checkContentsIn)
}

func newRowCompletion(c catalog) *Task[RowCompletionInput, RowCompletionAnswer] {
	return newTask("row_completion", rowCompletionVersion, system("rules", "attributes", "row_completion"), 4000, c.enums(),
		rowCompletionUser(c), checkRowCompletion(c), checkRowCompletionIn)
}

func newTextToItems(c catalog) *Task[TextToItemsInput, TextToItemsAnswer] {
	return newTask[TextToItemsInput, TextToItemsAnswer]("text_to_items", textToItemsVersion, system("rules", "attributes", "text_to_items"), 8000, c.enums(),
		textToItemsUser(c), checkTextToItems(c), nil)
}

func newCSVMapping(c catalog) *Task[CSVMappingInput, CSVMappingAnswer] {
	return newTask("csv_mapping", csvMappingVersion, system("rules", "csv_mapping"), 2000, c.enums(),
		csvMappingUser(c), checkCSVMapping(c), checkCSVMappingIn)
}

// Info describes a task without its types, for listings and tools.
type Info struct {
	Name, PromptVersion, Version, System string
	Schema                               []byte
}

func info[In, Out any](t *Task[In, Out]) Info {
	return Info{t.Name, t.PromptVersion, t.Version, t.System, t.Schema}
}

// All lists the seven tasks.
func All() []Info {
	return []Info{info(ItemDraft), info(Label), info(Receipt), info(Contents),
		info(RowCompletion), info(TextToItems), info(CSVMapping)}
}
