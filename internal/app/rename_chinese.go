package app

import (
	"bufio"
	"bytes"
	"errors"

	opencc "github.com/yanmingcao/opencc-go"
	"github.com/yanmingcao/opencc-go/pkg/conversion"
	"github.com/yanmingcao/opencc-go/pkg/dict"
	"github.com/yanmingcao/opencc-go/pkg/embeddata"
	"github.com/yanmingcao/opencc-go/pkg/segmentation"
)

type renameChineseConverter struct {
	steps []*opencc.Converter
}

func (c *renameChineseConverter) Convert(name string) string {
	for _, step := range c.steps {
		name = step.Convert(name)
	}
	return name
}

func newRenameChineseConverter(mode string) (*renameChineseConverter, error) {
	prefix, variant := "ST", ""
	switch mode {
	case "t2s":
		prefix = "TS"
	case "s2hk":
		variant = "HKVariants"
	case "s2tw":
		variant = "TWVariants"
	default:
		return nil, errors.New("不支持的繁简转换")
	}
	// Build only from the pinned embedded dictionaries, never local data overrides.
	load := func(name string) (dict.Dict, error) {
		raw, err := embeddata.GetDict(name)
		if err != nil {
			return nil, err
		}
		lexicon, err := dict.ParseLexiconFromReader(bufio.NewReader(bytes.NewReader(raw)))
		if err != nil {
			return nil, err
		}
		lexicon.Sort()
		return dict.NewTextDict(lexicon), nil
	}
	phrases, err := load(prefix + "Phrases")
	if err != nil {
		return nil, err
	}
	characters, err := load(prefix + "Characters")
	if err != nil {
		return nil, err
	}
	group := dict.NewDictGroup([]dict.Dict{phrases, characters})
	base := opencc.NewConverter(mode, segmentation.NewMaxMatchSegmentation(phrases),
		conversion.NewConversionChain([]*conversion.Conversion{conversion.NewConversion(group)}))
	result := &renameChineseConverter{steps: []*opencc.Converter{base}}
	if variant != "" {
		regional, err := load(variant)
		if err != nil {
			return nil, err
		}
		// Re-segment converted phrases so regional variants inside them also change.
		result.steps = append(result.steps, opencc.NewConverter(mode, segmentation.NewMaxMatchSegmentation(regional),
			conversion.NewConversionChain([]*conversion.Conversion{conversion.NewConversion(regional)})))
	}
	return result, nil
}
