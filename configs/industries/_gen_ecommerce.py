# -*- coding: utf-8 -*-
"""电商域行业四件套生成器（一次性）。
复用 food/fact-categories.json 作为「通用电商事实分类」骨架，逐行业微调+增补；
侧面复用通用电商 7 侧面；卖点维度与 P0 逐行业生成。运行后重写 ecommerce.json 的 nodes。
"""
import json, os

HERE = os.path.dirname(os.path.abspath(__file__))

def load_json(path):
    with open(os.path.join(HERE, path), encoding='utf-8') as f:
        return json.load(f)

def write_json(path, obj):
    with open(os.path.join(HERE, path), 'w', encoding='utf-8') as f:
        json.dump(obj, f, ensure_ascii=False, indent=2)
        f.write('\n')

def write_md(path, text):
    with open(os.path.join(HERE, path), 'w', encoding='utf-8') as f:
        f.write(text)

BASE_CATEGORIES = load_json('food/fact-categories.json')['categories']

# 通用电商侧面（7 个，与 food/apparel 对齐）
COMMON_SIDES = [
    {"code": "scenario", "name": "使用场景", "name_en": "Usage Scenario", "description": "什么场合、什么季节、怎么用"},
    {"code": "process", "name": "过程细节", "name_en": "Process Detail", "description": "材质/原料怎么选、工艺怎么做"},
    {"code": "value", "name": "利益算账", "name_en": "Value", "description": "划不划算、省了多少、性价比在哪"},
    {"code": "comparison", "name": "对比参照", "name_en": "Comparison", "description": "和别家、别的型号、别的原料的差别"},
    {"code": "pain_point", "name": "人群痛点", "name_en": "Pain Point", "description": "谁更需要、解决了什么顾虑"},
    {"code": "origin", "name": "品质来源", "name_en": "Origin", "description": "原料、品牌、产地的来源（只讲有事实的部分）"},
    {"code": "usage", "name": "使用方式", "name_en": "Usage", "description": "怎么用、怎么保存、怎么保养"},
]

COMMON_CATEGORY_SIDES = {
    "product": ["process", "origin"], "specification": ["scenario", "comparison"],
    "ingredients": ["process", "origin"], "craft": ["process"], "flavor": ["comparison", "scenario"],
    "functions": ["scenario"], "usage": ["usage"], "storage": ["usage"], "shelf_life": ["usage"],
    "product_experience": ["scenario"], "compatibility": ["scenario"],
    "link": ["value"], "variant": ["scenario", "comparison"], "trade": ["value"],
    "price": ["value", "comparison"], "promotion": ["value"], "gift": ["value"],
    "stock": ["pain_point"], "payment": ["value"],
    "fulfillment": ["comparison", "usage"], "shipping_time": ["usage"], "shipping_area": ["comparison"],
    "shipping_fee": ["value"], "delivery": ["usage"], "pickup": ["scenario"],
    "after_sales": ["pain_point"], "return_policy": ["pain_point"], "warranty": ["pain_point"],
    "customer_service": ["pain_point"], "service_content": ["scenario"],
    "identity_location": ["origin"], "brand": ["origin"], "qualification": ["origin"],
    "inspection": ["origin"], "origin": ["origin"], "stores": ["scenario"],
    "business_history": ["origin"], "audience": ["scenario", "pain_point"],
    "scenario": ["scenario"], "other": ["scenario"],
}

def make_categories(remove=(), override=None, extra=()):
    override = override or {}
    out = []
    for c in BASE_CATEGORIES:
        if c['value'] in remove:
            continue
        c = dict(c)
        if c['value'] in override:
            c['label'] = override[c['value']]
        out.append(c)
    out.extend(extra)
    return out

def make_sides(desc_overrides=None, category_sides_extra=None):
    desc_overrides = desc_overrides or {}
    sides = []
    for s in COMMON_SIDES:
        s = dict(s)
        if s['code'] in desc_overrides:
            s['description'] = desc_overrides[s['code']]
        sides.append(s)
    cs = dict(COMMON_CATEGORY_SIDES)
    if category_sides_extra:
        cs.update(category_sides_extra)
    return sides, cs

def md(name, name_en, industry_dimensions, total_note):
    dims = '\n'.join('%d. %s' % (i + 1, d) for i, d in enumerate(industry_dimensions))
    return f"""# {name}行业 · 卖点挖掘维度

根据正式事实包（商品卡 + 活动福利 + 事实依据），判定商品属于{name}行业后，严格套用下面的「核心必查维度」。只要事实包提供了对应信息，每个维度都要**各自独立成卖点**，不得合并进「其它」或直接省略。多个链接/商品时，每个商品都要有对应讲解角度，主推、组合装、尝鲜装等都不能遗漏。

## 通用维度（{name}行业也必须覆盖）

价格算账（原价/现价/活动价/组合价的可核算差价）、规格与选购（规格/型号/怎么选）、物流发货（快递/时效/包邮/配送范围）、售后保障（退换/赔付/运费承担）、品牌/资质/来源（品牌历史/产地/证书/检测）、目标人群与痛点（谁需要/解决什么顾虑）、使用/保存方式（怎么用/怎么保存）。

## {name}专属维度（逐个独立成卖点）

{dims}

## 判定规则

1. 只套用事实包能够支持的维度；某维度事实包没有对应信息时跳过，不得补写、不得编造。
2. 事实包明显属于{name}行业时，上述通用维度 + 专属维度一起构成本轮「核心必查维度」。
3. 事实包有多个商品/链接时，每个商品都必须有对应的讲解角度，主推、组合装、尝鲜装都不能遗漏。
4. {total_note}
"""

def p0(name, items):
    body = '\n\n'.join('%d. **%s**：%s' % (i + 1, t, d) for i, (t, d) in enumerate(items))
    return f"""# {name}行业 · 专属 P0 补充

在通用 P0 规则之上，{name}行业额外禁止以下内容（未确认正反都不说）：

{body}

补充说明：以上补充只增不减，与通用 P0 的四层结构（必说/可说/不提/改写）叠加执行；违反任意一条即触发【改写】或【不提】。
"""

# ============ 各行业定义 ============
# 每项: code, name, name_en, dims(专属维度列表), total(卖点总数目标),
#       remove(分类删除), override(分类label覆盖), extra(新增分类), extra_sides(新增侧面映射),
#       side_desc(侧面描述覆盖), p0_items(行业P0)

INDUSTRIES = [
    dict(
        code='fresh', name='生鲜果蔬', name_en='Fresh Produce',
        dims=[
            '**新鲜度与采摘**：采摘/捕捞日期、新鲜程度、现摘现发（仅限事实包有依据），单独成卖点；',
            '**品种与规格**：品种、大小、重量、份量、怎么选，单独成卖点；',
            '**产地与季节**：产地环境、当季时令、地域特色，单独成卖点；',
            '**口感风味**：甜度、水分、口感等（仅限已确认），单独成卖点；',
            '**储存与保鲜**：常温/冷藏保存、保鲜方法、保存期限，单独成卖点；',
            '**冷链物流**：冷链运输、发货时效、坏损赔付，单独成卖点；',
            '**资质/安全**：检测、无农残等（仅限事实包有依据），单独成卖点；',
        ],
        total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。新鲜度、产地、口感这些生鲜核心维度不得合并或省略。',
        extra=[{"value": "freshness", "label": "新鲜度 / 采摘信息", "label_en": "Freshness", "description": "采摘日期、新鲜程度与保鲜说明", "group": "商品信息", "group_en": "Product Info", "family": "product"},
               {"value": "variety", "label": "品种 / 规格", "label_en": "Variety", "description": "品种、大小、重量与份量", "group": "商品信息", "group_en": "Product Info", "family": "product"}],
        extra_sides={"freshness": ["origin", "scenario"], "variety": ["comparison", "scenario"]},
        side_desc={"usage": "怎么保鲜、怎么储存"},
        p0_items=[
            ('新鲜/有机绝对化', '不宣称「绝对新鲜、现摘现发、当天到、无农残、有机认证」，除非事实包提供对应检测/认证依据。'),
            ('治疗/保健功效', '不宣称「养生、排毒、增强免疫」等功效，生鲜按普通食品表述。'),
            ('无依据的产地/年份', '产地、当季、年份只讲事实包明确提供的，不延伸、不美化。'),
            ('无依据的稀缺/限时', '不说「限量 X 份、马上断货、最后一批」等未确认的紧迫话术。'),
        ],
    ),
    dict(
        code='beverage', name='酒水饮料', name_en='Beverages',
        dims=[
            '**口感风味**：口味、口感、香气、回甘等，单独成卖点；',
            '**原料与配料**：原料、配料、是否真材实料、无添加（仅限有依据），单独成卖点；',
            '**规格与度数**：容量、酒精度、规格、怎么选，单独成卖点；',
            '**产地与品牌**：产地、水源、酿造历史、品牌故事，单独成卖点；',
            '**饮用场景**：聚会、佐餐、送礼、日常，单独成卖点；',
            '**储存与饮用方式**：储存条件、适饮温度、饮用方法，单独成卖点；',
            '**资质/认证**：生产许可、质检（仅限事实包有依据），单独成卖点；',
        ],
        total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。口感、原料、产地这些核心维度不得合并或省略。',
        extra=[{"value": "alcohol", "label": "酒精度 / 度数", "label_en": "Alcohol", "description": "酒精度、度数、容量规格", "group": "商品信息", "group_en": "Product Info", "family": "product"},
               {"value": "taste", "label": "口感 / 香气", "label_en": "Taste", "description": "口味、香气、回甘等风味特点", "group": "商品信息", "group_en": "Product Info", "family": "product"}],
        extra_sides={"alcohol": ["specification", "comparison"], "taste": ["comparison", "scenario"]},
        p0_items=[
            ('医疗/保健功效', '不宣称「解酒、助眠、养生、降三高」等功效，酒水饮料按普通食品表述。'),
            ('未成年人相关', '不出现诱导未成年饮酒、不宣称「适合青少年」等表述。'),
            ('绝对化品质承诺', '不说「零添加、纯天然、绝对安全」，除非事实包有对应检测依据。'),
            ('无依据的年份/产地/度数', '年份、产地、度数、等级只讲事实包明确提供的，不延伸。'),
        ],
    ),
    dict(
        code='beauty', name='美妆个护', name_en='Beauty & Personal Care',
        dims=[
            '**成分与配方**：核心成分、配方、无刺激成分（仅限有依据），单独成卖点；',
            '**功效与作用**：功效宣称仅限有备案/检测依据的（保湿、清洁等基础功效），单独成卖点；',
            '**肤质适配**：适合什么肤质、什么人群，单独成卖点；',
            '**质地与肤感**：质地、肤感、吸收、香型，单独成卖点；',
            '**使用方法**：怎么用、用量、使用步骤，单独成卖点；',
            '**安全温和**：温和性、敏感肌可用（仅限有依据），单独成卖点；',
            '**资质/备案**：备案号、检测、认证（仅限事实包有依据），单独成卖点；',
        ],
        total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。成分、功效（仅备案）、肤质适配这些核心维度不得合并或省略。',
        extra=[{"value": "efficacy", "label": "功效 / 作用", "label_en": "Efficacy", "description": "已备案或已提供依据的功效说明", "group": "商品信息", "group_en": "Product Info", "family": "product"},
               {"value": "skin_type", "label": "适用肤质 / 人群", "label_en": "Skin Type", "description": "适用肤质、发质与适用人群", "group": "商品信息", "group_en": "Product Info", "family": "product"}],
        extra_sides={"efficacy": ["scenario", "comparison"], "skin_type": ["pain_point", "scenario"]},
        p0_items=[
            ('医疗功效宣称', '不宣称「祛斑、美白、抗衰、祛痘、治疗」等医疗功效，除非有相应备案/特证。'),
            ('绝对化安全承诺', '不说「零刺激、不过敏、绝对安全、适合所有人」，未确认的一律不提。'),
            ('成分与备案不一致', '成分、功效只讲与备案/事实包一致的内容，不夸大浓度、不虚标。'),
            ('无依据的销量/口碑', '不说「全网第一、销量最高」等未确认的排名。'),
        ],
    ),
    dict(
        code='home', name='家居日用', name_en='Home & Daily',
        dims=[
            '**材质与做工**：材质、做工、用料，单独成卖点；',
            '**功能与设计**：功能、设计风格、收纳/空间，单独成卖点；',
            '**耐用与品质**：耐用性、承重、使用年限（仅限有依据），单独成卖点；',
            '**规格与选购**：尺寸、容量、颜色、怎么选，单独成卖点；',
            '**使用场景**：居家、办公、收纳、季节场景，单独成卖点；',
            '**清洗保养**：怎么清洗、保养、替换，单独成卖点；',
            '**安全环保**：环保、无甲醛（仅限有检测依据），单独成卖点；',
        ],
        total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。材质、功能、耐用这些核心维度不得合并或省略。',
        extra=[{"value": "durability", "label": "耐用 / 承重 / 品质", "label_en": "Durability", "description": "耐用性、承重能力与品质说明", "group": "商品信息", "group_en": "Product Info", "family": "product"},
               {"value": "design", "label": "设计 / 风格", "label_en": "Design", "description": "设计风格、外观与空间适配", "group": "商品信息", "group_en": "Product Info", "family": "product"}],
        extra_sides={"durability": ["process", "pain_point"], "design": ["scenario", "comparison"]},
        p0_items=[
            ('绝对化环保承诺', '不宣称「零甲醛、绝对环保、无任何有害物质」，除非有检测报告。'),
            ('夸大耐用年限', '不虚标使用年限、承重，只讲事实包确认的。'),
            ('医疗/保健功效', '不宣称「助眠、养生、除菌治病」等功效，除非有检测依据。'),
        ],
    ),
    dict(
        code='appliance', name='家电数码', name_en='Appliances & Digital',
        dims=[
            '**核心参数**：核心参数、性能、配置，单独成卖点；',
            '**功能与体验**：功能、使用体验、便捷性，单独成卖点；',
            '**能效与功耗**：能效等级、功耗（仅限有依据），单独成卖点；',
            '**兼容与适配**：兼容设备、适配范围，单独成卖点；',
            '**安装与使用**：安装方式、使用步骤、操作，单独成卖点；',
            '**保修与售后**：质保期限、售后范围（仅讲确认的），单独成卖点；',
            '**品牌与资质**：品牌、认证、检测（仅限事实包有依据），单独成卖点；',
        ],
        total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。参数、功能、能效、保修这些核心维度不得合并或省略。',
        extra=[{"value": "energy_efficiency", "label": "能效 / 功耗", "label_en": "Energy Efficiency", "description": "能效等级、功耗与节能说明", "group": "商品信息", "group_en": "Product Info", "family": "product"},
               {"value": "installation", "label": "安装 / 使用方式", "label_en": "Installation", "description": "安装方式、使用步骤与操作说明", "group": "商品信息", "group_en": "Product Info", "family": "product"}],
        extra_sides={"energy_efficiency": ["value", "comparison"], "installation": ["usage", "scenario"]},
        p0_items=[
            ('虚标参数/能效', '不虚标功率、容量、能效等级、参数，只讲事实包/铭牌确认的。'),
            ('夸大保修', '不夸大质保年限、范围，只讲确认的保修政策。'),
            ('绝对化安全承诺', '不说「绝对安全、永不损坏」等，电器安全须有认证依据。'),
        ],
    ),
    dict(
        code='maternal', name='母婴用品', name_en='Maternal & Baby',
        dims=[
            '**材质安全**：材质、A类/食品级、无有害物质（仅限有检测依据），单独成卖点；',
            '**适用月龄**：适用月龄/年龄、成长阶段，单独成卖点；',
            '**功能与设计**：功能、人体工学、设计细节，单独成卖点；',
            '**安全认证**：安全标准、认证、检测（仅限事实包有依据），单独成卖点；',
            '**使用与清洗**：使用方式、清洗消毒、注意事项，单独成卖点；',
            '**规格与选购**：尺寸、容量、怎么选，单独成卖点；',
            '**品牌与口碑**：品牌、专业背书（仅讲确认的），单独成卖点；',
        ],
        total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。材质安全、适用月龄、安全认证这些母婴核心维度不得合并或省略。',
        extra=[{"value": "age_range", "label": "适用月龄 / 年龄", "label_en": "Age Range", "description": "适用月龄、年龄与成长阶段", "group": "商品信息", "group_en": "Product Info", "family": "product"},
               {"value": "material_safety", "label": "材质安全 / 等级", "label_en": "Material Safety", "description": "材质等级、A类/食品级与安全说明", "group": "商品信息", "group_en": "Product Info", "family": "product"}],
        extra_sides={"age_range": ["scenario", "pain_point"], "material_safety": ["process", "origin"]},
        p0_items=[
            ('绝对安全承诺', '不宣称「绝对安全、零刺激、无菌、无毒」，除非有检测/认证依据。'),
            ('发育/医疗功效', '不宣称「促进发育、增高、益智」等功效，未确认的一律不提。'),
            ('材质等级虚标', '材质等级、A类、食品级须与检测一致，不虚标。'),
            ('无依据的专业背书', '不说「医生推荐、权威认证」等未确认背书。'),
        ],
    ),
    dict(
        code='building', name='家装建材', name_en='Building Materials',
        dims=[
            '**材质与用料**：材质、用料、成分，单独成卖点；',
            '**环保等级**：环保等级、甲醛释放（仅限有检测报告），单独成卖点；',
            '**用量与覆盖**：用量、覆盖面积、规格换算，单独成卖点；',
            '**施工与安装**：施工方式、安装步骤、施工注意事项，单独成卖点；',
            '**耐用与性能**：耐用、防水/防火/承重等性能（仅限有依据），单独成卖点；',
            '**规格与选购**：规格、型号、怎么选，单独成卖点；',
            '**品牌与资质**：品牌、资质、检测（仅限事实包有依据），单独成卖点；',
        ],
        total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。材质、环保等级、施工这些核心维度不得合并或省略。',
        extra=[{"value": "coverage", "label": "用量 / 覆盖面积", "label_en": "Coverage", "description": "用量、覆盖面积与规格换算", "group": "商品信息", "group_en": "Product Info", "family": "product"},
               {"value": "installation", "label": "施工 / 安装", "label_en": "Installation", "description": "施工方式、安装步骤与注意事项", "group": "商品信息", "group_en": "Product Info", "family": "product"},
               {"value": "eco_grade", "label": "环保等级", "label_en": "Eco Grade", "description": "环保等级、甲醛释放与检测依据", "group": "商品信息", "group_en": "Product Info", "family": "product"}],
        extra_sides={"coverage": ["value", "comparison"], "installation": ["usage", "pain_point"], "eco_grade": ["origin", "process"]},
        p0_items=[
            ('零甲醛/绝对环保', '不宣称「零甲醛、零污染、绝对环保」，除非有检测报告。'),
            ('性能虚标', '不虚标防水、防火、承重、耐用年限，只讲确认的。'),
            ('检测报告张冠李戴', '检测报告须与实际商品对应，不套用无关报告。'),
        ],
    ),
    dict(
        code='book', name='图书文教', name_en='Books & Education',
        dims=[
            '**内容简介**：内容主题、亮点、适合谁读，单独成卖点；',
            '**作者与出版社**：作者、出版社、专业背景，单独成卖点；',
            '**适读年龄**：适读年龄、难度分级，单独成卖点；',
            '**版本与装帧**：版本、装帧、纸张、配套，单独成卖点；',
            '**赠品与配套**：赠品、配套资料、音视频，单独成卖点；',
            '**规格与选购**：册数、开本、怎么选，单独成卖点；',
            '**品牌/资质**：出版社资质、获奖（仅讲确认的），单独成卖点；',
        ],
        total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。内容、作者、适读年龄这些核心维度不得合并或省略。',
        extra=[{"value": "author", "label": "作者 / 编者", "label_en": "Author", "description": "作者、编者与专业背景", "group": "品牌资质", "group_en": "Brand & Credentials", "family": "identity_location"},
               {"value": "publisher", "label": "出版社 / 出品方", "label_en": "Publisher", "description": "出版社、出品方与资质", "group": "品牌资质", "group_en": "Brand & Credentials", "family": "identity_location"},
               {"value": "age_range", "label": "适读年龄", "label_en": "Age Range", "description": "适读年龄与难度分级", "group": "商品信息", "group_en": "Product Info", "family": "product"},
               {"value": "edition", "label": "版本 / 装帧", "label_en": "Edition", "description": "版本、装帧、纸张与配套", "group": "商品信息", "group_en": "Product Info", "family": "product"}],
        extra_sides={"author": ["origin"], "publisher": ["origin"], "age_range": ["scenario", "pain_point"], "edition": ["comparison", "scenario"]},
        p0_items=[
            ('销量/榜单虚标', '不说「销量第一、排行榜第一、全网畅销」等未确认数据。'),
            ('正版/独有夸大', '不宣称「唯一正版、独家授权」未确认的。'),
            ('效果夸大', '不宣称「读了就提分、包会」等教育效果承诺。'),
        ],
    ),
    dict(
        code='pet', name='宠物用品', name_en='Pet Supplies',
        dims=[
            '**适用宠物**：适用宠物类型、体型、年龄，单独成卖点；',
            '**材质与安全**：材质、无毒、耐咬（仅限有依据），单独成卖点；',
            '**功能与设计**：功能、设计、使用便捷，单独成卖点；',
            '**规格与选购**：规格、尺寸、怎么选，单独成卖点；',
            '**使用方式**：怎么用、怎么清洁、注意事项，单独成卖点；',
            '**品牌与口碑**：品牌、专业背书（仅讲确认的），单独成卖点；',
        ],
        total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。适用宠物、材质安全这些核心维度不得合并或省略。',
        extra=[{"value": "pet_type", "label": "适用宠物 / 体型", "label_en": "Pet Type", "description": "适用宠物类型、体型与年龄", "group": "商品信息", "group_en": "Product Info", "family": "product"}],
        extra_sides={"pet_type": ["scenario", "pain_point"]},
        p0_items=[
            ('治疗/保健功效', '不宣称「驱虫、治病、保健」等功效，除非有兽药/检测依据。'),
            ('绝对安全承诺', '不说「绝对无毒、绝不咬坏」等未确认承诺。'),
            ('无依据的专业背书', '不说「兽医推荐」等未确认背书。'),
        ],
    ),
    dict(
        code='sports', name='运动户外', name_en='Sports & Outdoor',
        dims=[
            '**功能与性能**：防水、透气、承重、防滑等性能（仅限有检测依据），单独成卖点；',
            '**材质与面料**：材质、面料、克重，单独成卖点；',
            '**适用运动**：适用运动类型、场景，单独成卖点；',
            '**规格与尺码**：尺码、规格、怎么选，单独成卖点；',
            '**耐用与做工**：耐用、做工、细节，单独成卖点；',
            '**安全与认证**：安全装备认证（仅限事实包有依据），单独成卖点；',
            '**品牌与口碑**：品牌、专业背书（仅讲确认的），单独成卖点；',
        ],
        total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。性能、材质、适用运动这些核心维度不得合并或省略。',
        extra=[{"value": "sport_type", "label": "适用运动 / 场景", "label_en": "Sport Type", "description": "适用运动类型、户外场景与用途", "group": "商品信息", "group_en": "Product Info", "family": "product"}],
        extra_sides={"sport_type": ["scenario", "pain_point"]},
        p0_items=[
            ('性能虚标', '不虚标防水、透气、承重、防滑等级，须有检测依据。'),
            ('安全装备资质', '安全防护类（头盔、护具）须有认证依据，不夸大防护能力。'),
            ('绝对化承诺', '不说「绝对防摔、永不开胶」等未确认承诺。'),
        ],
    ),
    dict(
        code='jewelry', name='珠宝配饰', name_en='Jewelry & Accessories',
        dims=[
            '**材质与成色**：材质、成色、克重，单独成卖点；',
            '**工艺与设计**：工艺、设计、款式，单独成卖点；',
            '**证书与鉴定**：鉴定证书、检测（仅限事实包有依据），单独成卖点；',
            '**规格与尺寸**：圈号、尺寸、怎么选，单独成卖点；',
            '**佩戴场景**：日常、婚嫁、送礼、收藏，单独成卖点；',
            '**保养方式**：怎么保养、清洁、存放，单独成卖点；',
            '**品牌与来源**：品牌、产地、来源（仅讲确认的），单独成卖点；',
        ],
        total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。材质、成色、证书这些核心维度不得合并或省略。',
        extra=[{"value": "material_grade", "label": "材质 / 成色 / 克重", "label_en": "Material Grade", "description": "材质、成色、克重与等级", "group": "商品信息", "group_en": "Product Info", "family": "product"},
               {"value": "certificate", "label": "鉴定证书 / 检测", "label_en": "Certificate", "description": "鉴定证书、检测报告与依据", "group": "品牌资质", "group_en": "Brand & Credentials", "family": "identity_location"},
               {"value": "ring_size", "label": "圈号 / 尺寸", "label_en": "Ring Size", "description": "圈号、尺寸与选购对应", "group": "商品信息", "group_en": "Product Info", "family": "product"}],
        extra_sides={"material_grade": ["process", "origin"], "certificate": ["origin"], "ring_size": ["scenario", "comparison"]},
        p0_items=[
            ('成色/克重虚标', '不虚标克重、成色、等级、含金量，须与证书/检测一致。'),
            ('证书张冠李戴', '鉴定证书须与实际商品一一对应，不套用无关证书。'),
            ('保值/增值夸大', '不宣称「保值、升值、稳赚」等投资收益承诺。'),
            ('绝对化稀缺', '不说「全网唯一、绝版」等未确认话术。'),
        ],
    ),
    dict(
        code='auto', name='汽车用品', name_en='Auto Supplies',
        dims=[
            '**适配车型**：适配车型、年份、规格，单独成卖点；',
            '**材质与做工**：材质、做工、用料，单独成卖点；',
            '**功能与性能**：功能、性能（仅限有依据），单独成卖点；',
            '**安装与使用**：安装方式、使用步骤、注意事项，单独成卖点；',
            '**耐用与品质**：耐用、寿命（仅讲确认的），单独成卖点；',
            '**安全与认证**：安全件认证（仅限事实包有依据），单独成卖点；',
            '**品牌与口碑**：品牌、专业背书（仅讲确认的），单独成卖点；',
        ],
        total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。适配车型、材质、安全这些核心维度不得合并或省略。',
        extra=[{"value": "vehicle_fit", "label": "适配车型 / 年份", "label_en": "Vehicle Fit", "description": "适配车型、年份与规格", "group": "商品信息", "group_en": "Product Info", "family": "product"},
               {"value": "installation", "label": "安装 / 使用方式", "label_en": "Installation", "description": "安装方式、使用步骤与注意事项", "group": "商品信息", "group_en": "Product Info", "family": "product"}],
        extra_sides={"vehicle_fit": ["scenario", "comparison"], "installation": ["usage", "pain_point"]},
        p0_items=[
            ('适配范围虚标', '不虚标适配车型/年份，适配关系须与事实包一致。'),
            ('安全件资质', '刹车、轮胎等安全件须有认证依据，不夸大性能。'),
            ('性能虚标', '不虚标承重、耐温、寿命等参数。'),
        ],
    ),
]

def gen_industry(ind):
    code = ind['code']
    d = os.path.join(HERE, code)
    os.makedirs(d, exist_ok=True)
    cats = make_categories(remove=ind.get('remove', ()), override=ind.get('override'), extra=ind.get('extra', ()))
    write_json(os.path.join(code, 'fact-categories.json'), {"schema_version": 1, "categories": cats})
    sides, cs = make_sides(ind.get('side_desc'), ind.get('extra_sides'))
    write_json(os.path.join(code, 'sides.json'), {"schema_version": 2, "sides": sides, "category_sides": cs})
    write_md(os.path.join(code, 'selling-dimensions.md'), md(ind['name'], ind['name_en'], ind['dims'], ind['total']))
    write_md(os.path.join(code, 'p0-extra.md'), p0(ind['name'], ind['p0_items']))
    print('generated', code)

def build_catalog():
    # 固定节点顺序：general 在前
    nodes = [{"code": "general", "name": "综合/其他", "name_en": "General",
              "selling_dimensions": "", "sides": "", "fact_categories": "", "p0_extra": "", "children": []}]
    # 已有行业 food / apparel 保持原路径
    existing = {
        'food': ("food", "食品", "Food"),
        'apparel': ("apparel", "服装鞋帽", "Apparel"),
    }
    order = ['food', 'apparel'] + [ind['code'] for ind in INDUSTRIES]
    for code in order:
        if code in existing:
            d, name, en = existing[code]
            node = {"code": code, "name": name, "name_en": en,
                    "selling_dimensions": f"{d}/selling-dimensions.md",
                    "sides": f"{d}/sides.json",
                    "fact_categories": f"{d}/fact-categories.json",
                    "p0_extra": f"{d}/p0-extra.md" if os.path.exists(os.path.join(HERE, d, 'p0-extra.md')) else "",
                    "children": []}
        else:
            ind = next(i for i in INDUSTRIES if i['code'] == code)
            d = ind['code']
            node = {"code": code, "name": ind['name'], "name_en": ind['name_en'],
                    "selling_dimensions": f"{d}/selling-dimensions.md",
                    "sides": f"{d}/sides.json",
                    "fact_categories": f"{d}/fact-categories.json",
                    "p0_extra": f"{d}/p0-extra.md",
                    "children": []}
        nodes.append(node)
    catalog = {"schema_version": 2, "biz_domain": "ecommerce", "name": "电商", "name_en": "E-commerce",
               "defaults": {"selling_dimensions": "", "sides": "", "fact_categories": "", "p0_extra": ""},
               "nodes": nodes}
    write_json('ecommerce.json', catalog)
    print('wrote ecommerce.json with', len(nodes), 'nodes')

if __name__ == '__main__':
    for ind in INDUSTRIES:
        gen_industry(ind)
    # 补 apparel 的 p0-extra.md
    write_md('apparel/p0-extra.md', p0('服装鞋帽', [
        ('绝对化品质承诺', '不宣称「零甲醛、绝对不褪色、永不缩水、100%不起球」，除非有检测依据；改为可核实的说法。'),
        ('材质/成分虚标', '面料成分、克重、等级（A类/婴幼儿级）须与检测一致，不虚标、不张冠李戴。'),
        ('医疗/保健功效', '不宣称「抗菌治病、保暖养生」等功效，服装按普通商品表述。'),
        ('无依据的销量/口碑', '不说「全网第一、销量最高」等未确认排名。'),
        ('无依据的稀缺/限时', '不说「限量、最后 X 件、马上断货」等未确认话术。'),
    ]))
    print('wrote apparel/p0-extra.md')
    build_catalog()
