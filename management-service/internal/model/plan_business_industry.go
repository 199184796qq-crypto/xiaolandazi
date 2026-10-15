package model

type PlanBusinessIndustry struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

type PlanBusinessSettings struct {
	BizDomain       string                            `json:"biz_domain"`
	Industries      map[string]string                 `json:"industries"`
	IndustryOptions map[string][]PlanBusinessIndustry `json:"industry_options"`
}

func PlanBusinessIndustryOptions(domain string) []PlanBusinessIndustry {
	switch domain {
	case "ecommerce":
		return []PlanBusinessIndustry{
			{"general", "综合/其他"}, {"food", "食品零食"}, {"fresh", "生鲜农产品"}, {"beverage", "酒水饮料"},
			{"apparel", "服装鞋帽"}, {"beauty", "美妆个护"}, {"home", "家居日用"}, {"appliance", "家电数码"},
			{"maternal", "母婴用品"}, {"building", "家装建材"}, {"book", "图书文教"}, {"pet", "宠物用品"},
			{"sports", "运动户外"}, {"jewelry", "珠宝配饰"}, {"auto", "汽车用品"},
		}
	case "local_service":
		return []PlanBusinessIndustry{
			{"general", "综合/其他"}, {"dining", "餐饮美食"}, {"hotel", "酒店住宿"}, {"travel", "旅游景点"},
			{"leisure", "休闲娱乐"}, {"beauty_care", "丽人美业"}, {"fitness", "运动健身"}, {"parenting", "亲子玩乐"},
			{"life_services", "生活服务"}, {"home_services", "家政家装"}, {"auto_services", "汽车服务"},
			{"pet_services", "宠物服务"}, {"wedding", "婚庆摄影"}, {"retail", "门店零售"}, {"education", "教育培训"},
		}
	}
	return nil
}

func ValidPlanBusinessIndustry(domain, industry string) bool {
	for _, item := range PlanBusinessIndustryOptions(domain) {
		if item.Code == industry {
			return true
		}
	}
	return false
}
