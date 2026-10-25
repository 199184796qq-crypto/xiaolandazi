package livecost

import (
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"strings"
	"time"
	"unicode"
)

const SettingsKey = "live_cost_config"
const MaxSettingsJSONBytes = 32_000
const MaxConfiguredMicros int64 = 1_000_000_000_000

type TokenTier struct {
	MaxInputTokens              int64 `json:"max_input_tokens"`
	InputMicrosPerMillion       int64 `json:"input_micros_per_million"`
	CachedInputMicrosPerMillion int64 `json:"cached_input_micros_per_million"`
	OutputMicrosPerMillion      int64 `json:"output_micros_per_million"`
}

type ModelPrice struct {
	Provider string      `json:"provider"`
	Model    string      `json:"model"`
	Tiers    []TokenTier `json:"tiers"`
}

// Settings is an operator price estimate, not a supplier invoice. Every amount
// uses integer micro-CNY; no floating-point currency enters the paid boundary.
type Settings struct {
	Version              int          `json:"version"`
	Revision             int64        `json:"revision"`
	TTSPriceMicrosPer10K int64        `json:"tts_price_micros_per_10k"`
	DefaultModelRates    []TokenTier  `json:"default_model_rates"`
	ModelPrices          []ModelPrice `json:"model_prices"`
	MainlineHourlyMicros int64        `json:"mainline_hourly_micros"`
	OptionalHourlyMicros int64        `json:"optional_hourly_micros"`
	TotalHourlyMicros    int64        `json:"total_hourly_micros"`
}

// SupplierPrice preserves the existing explicit provider/model/unit contract.
// It is attached to a request snapshot by the adapter, never inferred from a
// configured reference price or applied to a different supplier identity.
type SupplierPrice struct {
	Provider                    string `json:"provider"`
	Model                       string `json:"model"`
	Kind                        string `json:"kind"`
	Unit                        string `json:"unit"`
	CharacterMicros             int64  `json:"character_micros"`
	InputMicrosPerMillion       int64  `json:"input_micros_per_million"`
	CachedInputMicrosPerMillion int64  `json:"cached_input_micros_per_million"`
	OutputMicrosPerMillion      int64  `json:"output_micros_per_million"`
}

type PricingSnapshot struct {
	Revision             int64          `json:"revision"`
	TTSPriceMicrosPer10K int64          `json:"tts_price_micros_per_10k"`
	ModelTiers           []TokenTier    `json:"model_tiers"`
	Contract             *SupplierPrice `json:"contract,omitempty"`
	Config               *Settings      `json:"config,omitempty"`
}

func DefaultSettings() Settings {
	return Settings{
		Version: 1, TTSPriceMicrosPer10K: 800_000,
		DefaultModelRates: []TokenTier{{InputMicrosPerMillion: 200_000, CachedInputMicrosPerMillion: 40_000, OutputMicrosPerMillion: 800_000}},
		ModelPrices: []ModelPrice{{Provider: "*", Model: "qwen3.7-flash", Tiers: []TokenTier{
			{MaxInputTokens: 32_000, InputMicrosPerMillion: 200_000, CachedInputMicrosPerMillion: 40_000, OutputMicrosPerMillion: 800_000},
			{MaxInputTokens: 256_000, InputMicrosPerMillion: 600_000, CachedInputMicrosPerMillion: 120_000, OutputMicrosPerMillion: 2_400_000},
			{InputMicrosPerMillion: 1_200_000, CachedInputMicrosPerMillion: 240_000, OutputMicrosPerMillion: 4_800_000},
		}}},
		MainlineHourlyMicros: 1_000_000, OptionalHourlyMicros: 1_200_000, TotalHourlyMicros: 1_300_000,
	}
}

// DecodeSettings requires an explicit complete configuration. Only an absent
// database row uses DefaultSettings; corrupt or partial JSON is not a free rate.
func DecodeSettings(raw string) (Settings, error) {
	var settings Settings
	if len(raw) == 0 || len(raw) > MaxSettingsJSONBytes || rejectDuplicateJSONFields(raw) != nil {
		return settings, errors.New("计费配置格式错误或内容过长")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		return settings, errors.New("计费配置格式错误，请检查字段与整数金额")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return settings, errors.New("计费配置只能包含一份完整设置")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &fields) != nil || fields == nil {
		return settings, errors.New("计费配置必须是完整对象")
	}
	for _, key := range []string{"version", "revision", "tts_price_micros_per_10k", "default_model_rates", "model_prices", "mainline_hourly_micros", "optional_hourly_micros", "total_hourly_micros"} {
		if value, ok := fields[key]; !ok || string(value) == "null" {
			return settings, errors.New("计费配置字段不完整，请重新读取后保存")
		}
	}
	if err := requireExplicitTierFields(fields["default_model_rates"]); err != nil {
		return settings, err
	}
	var prices []map[string]json.RawMessage
	if json.Unmarshal(fields["model_prices"], &prices) != nil {
		return settings, errors.New("模型价格规则格式错误")
	}
	for _, price := range prices {
		for _, key := range []string{"provider", "model", "tiers"} {
			if value, ok := price[key]; !ok || string(value) == "null" {
				return settings, errors.New("模型价格规则字段不完整")
			}
		}
		if err := requireExplicitTierFields(price["tiers"]); err != nil {
			return settings, err
		}
	}
	return settings, settings.Validate()
}

func requireExplicitTierFields(raw json.RawMessage) error {
	var tiers []map[string]json.RawMessage
	if json.Unmarshal(raw, &tiers) != nil {
		return errors.New("模型阶梯价格格式错误")
	}
	for _, tier := range tiers {
		for _, key := range []string{"max_input_tokens", "input_micros_per_million", "cached_input_micros_per_million", "output_micros_per_million"} {
			if value, ok := tier[key]; !ok || string(value) == "null" {
				return errors.New("模型阶梯价格字段不完整，缓存价格须明确填写")
			}
		}
	}
	return nil
}

func rejectDuplicateJSONFields(raw string) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 32 {
			return errors.New("计费配置层级过深")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			keys := make(map[string]bool)
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := token.(string)
				key = strings.ToLower(key)
				if !ok || keys[key] {
					return errors.New("计费配置含重复字段")
				}
				keys[key] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("计费配置结构错误")
		}
		_, err = decoder.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("计费配置含多余内容")
	}
	return nil
}

func Validate(settings Settings) error { return settings.Validate() }

func (settings Settings) Validate() error {
	if settings.Version != 1 || settings.Revision < 0 || settings.Revision >= MaxConfiguredMicros {
		return errors.New("计费配置版本无效，请重新读取后保存")
	}
	if settings.TTSPriceMicrosPer10K <= 0 || settings.TTSPriceMicrosPer10K > MaxConfiguredMicros {
		return errors.New("语音单价必须大于零且不超过允许上限")
	}
	if settings.MainlineHourlyMicros < 0 || settings.OptionalHourlyMicros < settings.MainlineHourlyMicros || settings.TotalHourlyMicros < settings.OptionalHourlyMicros || settings.TotalHourlyMicros <= 0 || settings.TotalHourlyMicros > MaxConfiguredMicros {
		return errors.New("小时预算须满足主线目标不大于可选软线，可选软线不大于总目标，且总目标大于零")
	}
	if err := validateTokenTiers(settings.DefaultModelRates); err != nil {
		return err
	}
	if len(settings.ModelPrices) > 32 {
		return errors.New("模型价格规则不能超过三十二条")
	}
	seen := make(map[string]bool)
	for _, price := range settings.ModelPrices {
		if !validPriceIdentity(price.Provider, 96) || !validPriceIdentity(price.Model, 160) || strings.Contains(price.Model, "*") || (price.Provider != "*" && strings.Contains(price.Provider, "*")) {
			return errors.New("模型价格的供应商或模型名称无效")
		}
		key := strings.ToLower(price.Provider) + "\x00" + strings.ToLower(price.Model)
		if seen[key] {
			return errors.New("同一供应商与模型不能重复配置价格")
		}
		seen[key] = true
		if err := validateTokenTiers(price.Tiers); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(settings)
	if err != nil || len(raw) > MaxSettingsJSONBytes {
		return errors.New("计费配置内容过长")
	}
	return nil
}

func validPriceIdentity(value string, limit int) bool {
	return value != "" && len(value) <= limit && strings.TrimSpace(value) == value && strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) < 0
}

func validateTokenTiers(tiers []TokenTier) error {
	if len(tiers) == 0 || len(tiers) > 16 {
		return errors.New("模型价格必须配置一至十六个阶梯")
	}
	var previous int64
	for index, tier := range tiers {
		if tier.MaxInputTokens < 0 || tier.MaxInputTokens > MaxConfiguredMicros || (index == len(tiers)-1 && tier.MaxInputTokens != 0) || (index < len(tiers)-1 && tier.MaxInputTokens <= previous) {
			return errors.New("模型输入阶梯须严格递增，且最后一档须覆盖全部更长输入")
		}
		if tier.InputMicrosPerMillion <= 0 || tier.InputMicrosPerMillion > MaxConfiguredMicros || tier.CachedInputMicrosPerMillion < 0 || tier.CachedInputMicrosPerMillion > MaxConfiguredMicros || tier.OutputMicrosPerMillion <= 0 || tier.OutputMicrosPerMillion > MaxConfiguredMicros {
			return errors.New("模型输入及输出单价必须大于零，缓存单价不能为负，且均不得超过允许上限")
		}
		previous = tier.MaxInputTokens
	}
	return nil
}

func (settings Settings) Policy() Policy {
	return Policy{Configured: true, Window: time.Hour, MainlineTargetMicros: settings.MainlineHourlyMicros, OptionalLimitMicros: settings.OptionalHourlyMicros, TotalTargetMicros: settings.TotalHourlyMicros}
}

func (settings Settings) Clone() Settings {
	copy := settings
	copy.DefaultModelRates = append([]TokenTier(nil), settings.DefaultModelRates...)
	copy.ModelPrices = make([]ModelPrice, len(settings.ModelPrices))
	for index, price := range settings.ModelPrices {
		copy.ModelPrices[index] = price
		copy.ModelPrices[index].Tiers = append([]TokenTier(nil), price.Tiers...)
	}
	return copy
}

func (snapshot PricingSnapshot) Validate() error {
	if snapshot.Revision < 0 || snapshot.Revision >= MaxConfiguredMicros || snapshot.TTSPriceMicrosPer10K <= 0 || snapshot.TTSPriceMicrosPer10K > MaxConfiguredMicros {
		return errors.New("请求价格快照的版本或语音单价无效")
	}
	if err := validateTokenTiers(snapshot.ModelTiers); err != nil {
		return err
	}
	if snapshot.Config != nil {
		if err := snapshot.Config.Validate(); err != nil {
			return err
		}
		if snapshot.Config.Revision != snapshot.Revision || snapshot.Config.TTSPriceMicrosPer10K != snapshot.TTSPriceMicrosPer10K {
			return errors.New("请求价格快照与配置版本不一致")
		}
	}
	if contract := snapshot.Contract; contract != nil {
		if !validPriceIdentity(contract.Provider, 96) || !validPriceIdentity(contract.Model, 160) || contract.Provider == "*" || strings.Contains(contract.Model, "*") {
			return errors.New("请求供应商价格身份无效")
		}
		for _, amount := range []int64{contract.CharacterMicros, contract.InputMicrosPerMillion, contract.CachedInputMicrosPerMillion, contract.OutputMicrosPerMillion} {
			if amount < 0 || amount > MaxConfiguredMicros {
				return errors.New("请求供应商价格超出允许范围")
			}
		}
		if contract.Unit == "characters" {
			if contract.Kind != KindTTS || contract.CharacterMicros <= 0 || contract.CharacterMicros > MaxConfiguredMicros {
				return errors.New("请求供应商字符价格无效")
			}
		} else if contract.Unit == "tokens" && (contract.Kind == KindModel || contract.Kind == KindTTS) {
			if err := validateTokenTiers([]TokenTier{{InputMicrosPerMillion: contract.InputMicrosPerMillion, CachedInputMicrosPerMillion: contract.CachedInputMicrosPerMillion, OutputMicrosPerMillion: contract.OutputMicrosPerMillion}}); err != nil {
				return err
			}
		} else {
			return errors.New("请求供应商计价单位无效")
		}
	}
	raw, err := json.Marshal(snapshot)
	if err != nil || len(raw) > 48_000 {
		return errors.New("请求价格快照内容过长")
	}
	return nil
}

// Snapshot copies the complete ladder, so actual response usage can choose a
// later tier without consulting a newly edited configuration.
func (settings Settings) Snapshot(provider, model string) PricingSnapshot {
	provider, model = strings.ToLower(strings.TrimSpace(provider)), strings.ToLower(strings.TrimSpace(model))
	tiers := settings.DefaultModelRates
	bestProvider, bestLength := -1, -1
	for _, price := range settings.ModelPrices {
		priceProvider, priceModel := strings.ToLower(strings.TrimSpace(price.Provider)), strings.ToLower(strings.TrimSpace(price.Model))
		providerRank := 0
		if priceProvider == provider {
			providerRank = 1
		} else if priceProvider != "*" {
			continue
		}
		if model != priceModel && !strings.HasPrefix(model, priceModel+"-") {
			continue
		}
		if providerRank > bestProvider || (providerRank == bestProvider && len(priceModel) > bestLength) {
			tiers, bestProvider, bestLength = price.Tiers, providerRank, len(priceModel)
		}
	}
	copy := settings.Clone()
	return PricingSnapshot{Revision: settings.Revision, TTSPriceMicrosPer10K: settings.TTSPriceMicrosPer10K, ModelTiers: append([]TokenTier(nil), tiers...), Config: &copy}
}

func TokenRates(tiers []TokenTier, input int64) (int64, int64, int64) {
	for _, tier := range tiers {
		if tier.MaxInputTokens == 0 || input <= tier.MaxInputTokens {
			return tier.InputMicrosPerMillion, tier.CachedInputMicrosPerMillion, tier.OutputMicrosPerMillion
		}
	}
	return 0, 0, 0 // Callers must validate settings before publishing a snapshot.
}

func EstimateTTSPer10K(characters, price int64) (int64, error) {
	if characters < 0 || price <= 0 || price > MaxConfiguredMicros {
		return 0, errors.New("语音费用计量或单价无效")
	}
	amount := new(big.Int).Mul(big.NewInt(characters), big.NewInt(price))
	amount.Add(amount, big.NewInt(9_999)).Quo(amount, big.NewInt(10_000))
	if !amount.IsInt64() {
		return 0, errors.New("语音费用计算越界")
	}
	return amount.Int64(), nil
}
