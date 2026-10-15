# -*- coding: utf-8 -*-
"""本地生活域行业四件套生成器。
复用 restaurant/sides.json 的 9 侧面 + restaurant/fact-categories.json 的通用骨架
（location/environment/service/reputation/brand/price/per_capita/appointment/validity/
redemption/audience/scenario/other），每行业叠加 4 个「内容/服务项目」专属类目。
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

# 通用骨架类目（从 restaurant 抽取，去掉套餐/口味/食材/做法 4 个内容类目）
SKELETON_VALUES = ['location', 'environment', 'service', 'reputation', 'brand',
                   'price', 'per_capita', 'appointment', 'validity', 'redemption',
                   'audience', 'scenario', 'other']
REST_CATS = {c['value']: c for c in load_json('restaurant/fact-categories.json')['categories']}

# 通用 9 侧面（复用 restaurant）
REST_SIDES = load_json('restaurant/sides.json')
COMMON_SIDES = REST_SIDES['sides']
COMMON_CATEGORY_SIDES = dict(REST_SIDES['category_sides'])

def make_categories(content_cats, skeleton_override=None):
    skeleton_override = skeleton_override or {}
    out = [dict(content_cats)] if isinstance(content_cats, dict) else [dict(c) for c in content_cats]
    for v in SKELETON_VALUES:
        c = dict(REST_CATS[v])
        if v in skeleton_override:
            c['label'] = skeleton_override[v]
        out.append(c)
    return out

def md(name, dims, total_note):
    dims_txt = '\n'.join('%d. %s' % (i + 1, d) for i, d in enumerate(dims))
    return f"""# {name}行业 · 卖点挖掘维度

根据正式事实包（套餐卡/项目卡 + 团购福利 + 事实依据），判定属于「本地生活 · {name}」行业后，严格套用下面的「核心必查维度」。只要事实包提供了对应信息，每个维度都要**各自独立成卖点**，不得合并进「其它」或直接省略。一个方案有多个套餐/项目时，每个都要有对应讲解角度。

## 核心必查维度（逐个独立成卖点）

{dims_txt}

## 判定规则

1. 只套用事实包能够支持的维度；某维度事实包没有对应信息时跳过，不得补写、不得编造。
2. 事实包明显属于{name}行业时，上述维度构成本轮「核心必查维度」。
3. 事实包有多个套餐/项目/链接时，每个都必须有对应的讲解角度，主推、尝鲜都不能遗漏。
4. {total_note}
5. **到店团购的铁律**：只讲「到店体验」，不涉及「外卖/快递配送」；配送范围、时效、打包、配送费属于外卖或电商物流，本行业不提。
"""

def p0(name, items):
    body = '\n\n'.join('%d. **%s**：%s' % (i + 1, t, d) for i, (t, d) in enumerate(items))
    return f"""# {name}行业 · 专属 P0 补充

在通用 P0 规则之上，本地生活{name}额外禁止以下内容（未确认正反都不说）：

{body}

补充说明：以上补充只增不减，与通用 P0 的四层结构（必说/可说/不提/改写）叠加执行；违反任意一条即触发【改写】或【不提】。到店团购的「承诺类」事实是最高危区，商家未书面确认的一律不进话术。
"""

def content_cat(value, label, label_en, desc, group='套餐信息', group_en='Package', family='product'):
    return {"value": value, "label": label, "label_en": label_en, "description": desc,
            "group": group, "group_en": group_en, "family": family}

INDUSTRIES = [
    dict(code='hotel', name='酒店住宿', name_en='Hotel',
         content=[content_cat('room', '房型 / 床型', 'Room Type', '房型、床型、面积与可住人数'),
                  content_cat('facility', '设施 / 配套', 'Facility', '餐饮、WiFi、停车、泳池等配套设施'),
                  content_cat('checkin', '入住 / 退房规则', 'Check-in', '入住退房时间、押金与到店核销'),
                  content_cat('surrounding', '周边 / 位置', 'Surrounding', '周边商圈、景点、交通与停车')],
         dims=['**价格算账**：团购价 vs 到店原价、每晚均价、含不含早、省多少，单独成卖点；',
               '**房型内容**：房型、床型、面积、可住几人、含不含早餐，单独成卖点；',
               '**地理位置**：位置、商圈、交通、地铁口、停车，单独成卖点；',
               '**设施配套**：WiFi、泳池、健身房、停车场等，单独成卖点；',
               '**服务体验**：前台、清洁、客房服务、入住退房流程，单独成卖点；',
               '**预约与使用**：需不需预约、入住退房时间、有效期，单独成卖点；',
               '**核销方式**：到店出示券码、押金、无隐形消费（仅限已确认），单独成卖点；',
               '**口碑与评价**：好评、评分、回头客，单独成卖点；',
               '**场景适配**：出差/度假/亲子/情侣，单独成卖点；'],
         total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。价格、房型、位置、口碑这些核心维度不得合并或省略。',
         p0_items=[('无隐形消费承诺', '不宣称「无隐形消费、到店不加价」，除非有确认依据。'),
                   ('节假日/时段通用', '不宣称「节假日通用、旺季可用」，未确认不提。'),
                   ('退款/过期承诺', '不宣称「过期可退、随时退、无条件退」，未确认不提。'),
                   ('无依据排名', '不宣称「最好、评分最高、最舒适」等，只讲事实包提供的评分与好评。')]),
    dict(code='travel', name='旅游景点', name_en='Travel & Scenic',
         content=[content_cat('ticket', '门票 / 票种', 'Ticket', '门票类型、成人/儿童票、套票'),
                  content_cat('route', '线路 / 游玩项目', 'Route', '游玩线路、项目、景点内容'),
                  content_cat('opening', '开放时间 / 场次', 'Opening', '开放时间、场次与入园规则'),
                  content_cat('transport', '交通 / 到达', 'Transport', '交通方式、停车、摆渡与到达指引')],
         dims=['**价格算账**：团购价 vs 现场价、套票省多少、人均，单独成卖点；',
               '**门票内容**：票种、包含项目、成人/儿童票、是否通票，单独成卖点；',
               '**游玩线路**：线路、项目、景点亮点，单独成卖点；',
               '**开放时间**：开放时间、场次、入园规则，单独成卖点；',
               '**位置与交通**：位置、交通、停车、摆渡，单独成卖点；',
               '**预约与使用**：需不需预约、有效期、节假日是否通用（仅已确认），单独成卖点；',
               '**核销方式**：到园出示券码、兑换流程，单独成卖点；',
               '**口碑与评价**：好评、评分、热度，单独成卖点；',
               '**场景适配**：亲子/情侣/家庭/团建，单独成卖点；'],
         total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。门票、线路、时间、位置这些核心维度不得合并或省略。',
         p0_items=[('节假日通用承诺', '不宣称「节假日通用、不限场次」，未确认不提。'),
                   ('退款/改期承诺', '不宣称「随时退、无条件退、可改期」，未确认不提。'),
                   ('无依据热度', '不宣称「必打卡、全网最火、排队王」等，只讲事实包提供的。'),
                   ('安全承诺', '不宣称「绝对安全、零风险」，游乐项目安全须知按事实包说明。')]),
    dict(code='leisure', name='休闲娱乐', name_en='Leisure & Entertainment',
         content=[content_cat('program', '项目 / 玩法', 'Program', '娱乐项目、玩法与套餐内容'),
                  content_cat('session', '场次 / 时长', 'Session', '场次、时长、人数与包间规则'),
                  content_cat('facility', '设施 / 环境', 'Facility', '设备、包间、环境与配套'),
                  content_cat('rules', '使用规则 / 限制', 'Rules', '使用规则、限制条件与附加消费')],
         dims=['**价格算账**：团购价 vs 现场价、人均、省多少，单独成卖点；',
               '**项目内容**：项目、玩法、套餐包含什么，单独成卖点；',
               '**场次时长**：场次、时长、包间/人数规则，单独成卖点；',
               '**设施环境**：设备、包间、环境、氛围，单独成卖点；',
               '**服务体验**：接待、服务、体验流程，单独成卖点；',
               '**预约与使用**：需不需预约、有效期、节假日是否通用（仅已确认），单独成卖点；',
               '**核销方式**：到店出示券码、附加消费（仅已确认），单独成卖点；',
               '**口碑与评价**：好评、评分、回头客，单独成卖点；',
               '**场景适配**：约会/聚会/亲子/团建，单独成卖点；'],
         total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。项目、场次、环境、口碑这些核心维度不得合并或省略。',
         p0_items=[('节假日通用承诺', '不宣称「节假日通用、随时可用」，未确认不提。'),
                   ('无隐形消费', '不宣称「无附加消费」，除非有确认依据。'),
                   ('退款/过期承诺', '不宣称「随时退、无条件退」，未确认不提。'),
                   ('无依据排名', '不宣称「最好玩、评分最高」等，只讲事实包提供的。')]),
    dict(code='beauty_care', name='丽人美业', name_en='Beauty & Salon',
         content=[content_cat('program', '服务项目', 'Program', '美发、美甲、美容项目与套餐内容'),
                  content_cat('stylist', '技师 / 手艺', 'Stylist', '技师资质、手艺与经验（仅已确认）'),
                  content_cat('effect', '效果 / 风格', 'Effect', '可核验的效果说明与风格（受合规约束）'),
                  content_cat('product', '产品 / 用料', 'Product', '使用产品、用料与品牌（仅已确认）')],
         dims=['**价格算账**：团购价 vs 到店价、项目省多少，单独成卖点；',
               '**服务项目**：项目内容、步骤、包含什么，单独成卖点；',
               '**技师与手艺**：技师资质、手艺、经验（仅已确认），单独成卖点；',
               '**环境与体验**：环境、服务、体验流程，单独成卖点；',
               '**产品用料**：使用的产品、用料、品牌（仅已确认），单独成卖点；',
               '**预约与使用**：需不需预约、有效期、到店安排，单独成卖点；',
               '**核销方式**：到店出示券码、附加消费（仅已确认），单独成卖点；',
               '**口碑与评价**：好评、评分、回头客，单独成卖点；',
               '**场景适配**：日常/约会/婚礼/重要场合，单独成卖点；'],
         total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。项目、技师、环境、口碑这些核心维度不得合并或省略。',
         p0_items=[('效果夸大', '不宣称「一次见效、永久保持、无副作用」，效果类宣称须有依据。'),
                   ('医疗功效', '不宣称「治疗、祛斑、祛痘」等医疗功效，未确认不提。'),
                   ('产品资质虚标', '使用产品、品牌、资质只讲事实包确认的。'),
                   ('无隐形消费', '不宣称「无附加消费」，除非有确认依据。')]),
    dict(code='fitness', name='运动健身', name_en='Fitness',
         content=[content_cat('course', '课程 / 卡种', 'Course', '课程、卡种、次数与有效期'),
                  content_cat('coach', '教练 / 资质', 'Coach', '教练资质、经验（仅已确认）'),
                  content_cat('equipment', '器械 / 设施', 'Equipment', '器械、场馆、设施与配套'),
                  content_cat('membership', '会员 / 使用规则', 'Membership', '会员权益、使用规则与限制')],
         dims=['**价格算账**：团购价 vs 门店价、单次均价、省多少，单独成卖点；',
               '**课程卡种**：课程、卡种、次数、有效期，单独成卖点；',
               '**教练资质**：教练资质、经验（仅已确认），单独成卖点；',
               '**场馆设施**：器械、场馆、淋浴、配套，单独成卖点；',
               '**服务体验**：接待、指导、体验流程，单独成卖点；',
               '**预约与使用**：需不需预约、有效期、使用规则，单独成卖点；',
               '**核销方式**：到店出示券码、附加消费（仅已确认），单独成卖点；',
               '**口碑与评价**：好评、评分、会员回头，单独成卖点；',
               '**场景适配**：减脂/增肌/塑形/康复，单独成卖点；'],
         total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。课程、教练、场馆、口碑这些核心维度不得合并或省略。',
         p0_items=[('效果夸大', '不宣称「月瘦 X 斤、包瘦、包练出马甲线」等效果承诺。'),
                   ('医疗/康复功效', '不宣称「治疗、康复」等医疗功效，未确认不提。'),
                   ('资质虚标', '教练资质、认证只讲事实包确认的。'),
                   ('退款/转让承诺', '不宣称「随时退、可转让」，未确认不提。')]),
    dict(code='parenting', name='亲子玩乐', name_en='Parent-Child',
         content=[content_cat('program', '游乐项目', 'Program', '游乐项目、套餐与玩法'),
                  content_cat('age_range', '适龄 / 身高', 'Age Range', '适龄、身高限制与陪同要求'),
                  content_cat('safety', '安全 / 防护', 'Safety', '安全措施、防护与保障（仅已确认）'),
                  content_cat('accompany', '陪同 / 使用规则', 'Accompany', '陪同规则、使用限制与营业时间')],
         dims=['**价格算账**：团购价 vs 现场价、人均、省多少，单独成卖点；',
               '**游乐项目**：项目、玩法、套餐包含什么，单独成卖点；',
               '**适龄范围**：适龄、身高限制、陪同要求，单独成卖点；',
               '**安全防护**：安全措施、防护、保障（仅已确认），单独成卖点；',
               '**设施环境**：场地、环境、卫生、配套，单独成卖点；',
               '**预约与使用**：需不需预约、有效期、营业时间，单独成卖点；',
               '**核销方式**：到店出示券码、附加消费（仅已确认），单独成卖点；',
               '**口碑与评价**：好评、评分、回头客，单独成卖点；',
               '**场景适配**：周末遛娃/生日/家庭出游，单独成卖点；'],
         total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。项目、适龄、安全、口碑这些核心维度不得合并或省略。',
         p0_items=[('绝对安全承诺', '不宣称「绝对安全、零风险、不会受伤」，除非有确认依据。'),
                   ('医疗/发育功效', '不宣称「开发智力、促进发育」等，未确认不提。'),
                   ('节假日通用', '不宣称「节假日通用、随时可用」，未确认不提。'),
                   ('无依据排名', '不宣称「最好玩、最热门」等，只讲事实包提供的。')]),
    dict(code='life_services', name='生活服务', name_en='Life Services',
         content=[content_cat('service_item', '服务项目', 'Service Item', '洗衣、维修、回收等服务项目'),
                  content_cat('door_service', '上门 / 时效', 'Door Service', '是否上门、服务时效与范围'),
                  content_cat('material', '用料 / 配件', 'Material', '用料、配件与品牌（仅已确认）'),
                  content_cat('coverage', '服务范围 / 保障', 'Coverage', '服务范围、保障与赔付规则')],
         dims=['**价格算账**：团购价 vs 门店价、服务省多少，单独成卖点；',
               '**服务项目**：服务内容、步骤、包含什么，单独成卖点；',
               '**上门时效**：是否上门、时效、服务范围，单独成卖点；',
               '**用料配件**：用料、配件、品牌（仅已确认），单独成卖点；',
               '**服务保障**：保障、赔付、售后（仅已确认），单独成卖点；',
               '**预约与使用**：需不需预约、有效期、服务时间，单独成卖点；',
               '**核销方式**：到店/上门核销、附加费用（仅已确认），单独成卖点；',
               '**口碑与评价**：好评、评分、回头客，单独成卖点；'],
         total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。项目、时效、保障、口碑这些核心维度不得合并或省略。',
         p0_items=[('效果/结果承诺', '不宣称「洗得绝对干净、修好为止」等结果承诺，除非有确认保障。'),
                   ('上门范围虚标', '上门范围、时效只讲事实包确认的，不虚标。'),
                   ('赔付/保障夸大', '赔付、保障规则须与事实包一致。'),
                   ('资质虚标', '维修资质、品牌只讲确认的。')]),
    dict(code='home_services', name='家政家装', name_en='Home Services',
         content=[content_cat('service_item', '服务项目', 'Service Item', '保洁、保姆、维修、装修等服务项目'),
                  content_cat('staff', '人员 / 资质', 'Staff', '人员资质、培训与背景（仅已确认）'),
                  content_cat('door_service', '上门 / 时效', 'Door Service', '是否上门、时效、服务范围'),
                  content_cat('material', '用料 / 配件', 'Material', '用料、配件、辅材与品牌（仅已确认）')],
         dims=['**价格算账**：团购价 vs 门店价、服务省多少，单独成卖点；',
               '**服务项目**：服务内容、步骤、包含什么，单独成卖点；',
               '**人员资质**：人员资质、培训、经验（仅已确认），单独成卖点；',
               '**上门时效**：是否上门、时效、服务范围，单独成卖点；',
               '**用料配件**：用料、配件、辅材、品牌（仅已确认），单独成卖点；',
               '**服务保障**：保障、售后、赔付（仅已确认），单独成卖点；',
               '**预约与使用**：需不需预约、有效期、服务时间，单独成卖点；',
               '**口碑与评价**：好评、评分、回头客，单独成卖点；'],
         total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。项目、人员、时效、口碑这些核心维度不得合并或省略。',
         p0_items=[('人员背景虚标', '人员资质、背景、持证只讲事实包确认的。'),
                   ('效果承诺', '不宣称「一次保洁永无灰尘、修好为止」等，除非有确认保障。'),
                   ('环保绝对化', '不宣称「零甲醛、绝对环保」，除非有检测报告。'),
                   ('赔付/保障夸大', '赔付、保障规则须与事实包一致。')]),
    dict(code='auto_services', name='汽车服务', name_en='Auto Services',
         content=[content_cat('service_item', '服务项目', 'Service Item', '洗车、保养、维修、美容等服务项目'),
                  content_cat('technician', '技师 / 资质', 'Technician', '技师资质、经验（仅已确认）'),
                  content_cat('parts', '配件 / 材料', 'Parts', '配件、油品、材料与品牌（仅已确认）'),
                  content_cat('quality', '质保 / 保障', 'Quality', '质保、保障与售后（仅已确认）')],
         dims=['**价格算账**：团购价 vs 门店价、服务省多少，单独成卖点；',
               '**服务项目**：服务内容、步骤、包含什么，单独成卖点；',
               '**技师资质**：技师资质、经验（仅已确认），单独成卖点；',
               '**配件材料**：配件、油品、材料、品牌（仅已确认），单独成卖点；',
               '**质保保障**：质保、售后、保障（仅已确认），单独成卖点；',
               '**预约与使用**：需不需预约、有效期、服务时长，单独成卖点；',
               '**核销方式**：到店出示券码、附加费用（仅已确认），单独成卖点；',
               '**口碑与评价**：好评、评分、回头客，单独成卖点；'],
         total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。项目、技师、配件、质保这些核心维度不得合并或省略。',
         p0_items=[('配件资质虚标', '配件、油品、品牌、真伪只讲事实包确认的。'),
                   ('效果承诺', '不宣称「洗得绝对干净、修好不再坏」等，除非有确认质保。'),
                   ('质保夸大', '质保范围、期限须与事实包一致。'),
                   ('适配虚标', '适用车型、配件适配只讲确认的。')]),
    dict(code='pet_services', name='宠物服务', name_en='Pet Services',
         content=[content_cat('service_item', '服务项目', 'Service Item', '洗护、寄养、医疗、美容等服务项目'),
                  content_cat('qualification', '资质 / 医疗', 'Qualification', '医疗资质、兽医资质（仅已确认）'),
                  content_cat('environment', '环境 / 设施', 'Environment', '环境、卫生、设施与安全'),
                  content_cat('safety', '安全 / 保障', 'Safety', '安全措施、保障与注意事项')],
         dims=['**价格算账**：团购价 vs 门店价、服务省多少，单独成卖点；',
               '**服务项目**：服务内容、步骤、包含什么，单独成卖点；',
               '**资质医疗**：兽医资质、医疗资质（仅已确认），单独成卖点；',
               '**环境设施**：环境、卫生、设施、安全，单独成卖点；',
               '**服务体验**：接待、照顾、体验流程，单独成卖点；',
               '**预约与使用**：需不需预约、有效期、服务时间，单独成卖点；',
               '**核销方式**：到店出示券码、附加费用（仅已确认），单独成卖点；',
               '**口碑与评价**：好评、评分、回头客，单独成卖点；'],
         total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。项目、资质、环境、口碑这些核心维度不得合并或省略。',
         p0_items=[('医疗功效', '不宣称「治病、驱虫、保健」等，除非有兽药/医疗资质依据。'),
                   ('绝对安全承诺', '不宣称「绝对安全、零风险、不会应激」，未确认不提。'),
                   ('资质虚标', '兽医资质、医疗资质只讲事实包确认的。'),
                   ('无依据口碑', '不宣称「最好、最专业」等，只讲事实包提供的。')]),
    dict(code='wedding', name='婚庆摄影', name_en='Wedding & Photography',
         content=[content_cat('package', '套系 / 内容', 'Package', '套系内容、拍摄项目、婚礼服务'),
                  content_cat('style', '风格 / 作品', 'Style', '风格、样片、作品（仅已确认）'),
                  content_cat('dress', '服装 / 造型', 'Dress', '服装、造型、妆面与道具'),
                  content_cat('venue', '场地 / 档期', 'Venue', '场地、档期与拍摄地点')],
         dims=['**价格算账**：团购价 vs 门店价、套系省多少，单独成卖点；',
               '**套系内容**：套系、项目、包含什么，单独成卖点；',
               '**风格作品**：风格、样片、作品（仅已确认），单独成卖点；',
               '**服装造型**：服装、造型、妆面、道具，单独成卖点；',
               '**场地档期**：场地、档期、拍摄地点，单独成卖点；',
               '**预约与使用**：需不需预约、有效期、档期安排，单独成卖点；',
               '**核销方式**：到店核销、附加费用（仅已确认），单独成卖点；',
               '**口碑与评价**：好评、评分、案例，单独成卖点；',
               '**场景适配**：婚礼/婚纱照/写真/全家福，单独成卖点；'],
         total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。套系、风格、服装、档期这些核心维度不得合并或省略。',
         p0_items=[('效果承诺', '不宣称「张张大片、包满意」等，效果须以样片为依据。'),
                   ('价格虚标', '套系价格、附加费用须与事实包一致，不虚标、不隐瞒。'),
                   ('档期承诺', '不宣称「随时有档期、指定日期」，未确认不提。'),
                   ('退款/改期', '不宣称「无条件退、随便改期」，未确认不提。')]),
    dict(code='retail', name='门店零售', name_en='Retail Store',
         content=[content_cat('goods', '商品 / 品类', 'Goods', '在售商品、品类与品牌'),
                  content_cat('activity', '门店活动', 'Activity', '门店活动、折扣与会员权益'),
                  content_cat('location', '门店位置', 'Location', '门店地址、商圈、交通与停车'),
                  content_cat('service', '门店服务', 'Service', '导购、试衣、退换等服务')],
         dims=['**价格算账**：团购/活动价 vs 门店价、省多少，单独成卖点；',
               '**商品品类**：在售商品、品类、品牌，单独成卖点；',
               '**门店活动**：活动、折扣、会员权益，单独成卖点；',
               '**门店位置**：地址、商圈、交通、停车，单独成卖点；',
               '**门店服务**：导购、试衣、退换、体验，单独成卖点；',
               '**预约与使用**：需不需预约、有效期、营业时间，单独成卖点；',
               '**核销方式**：到店核销、附加费用（仅已确认），单独成卖点；',
               '**口碑与评价**：好评、评分、回头客，单独成卖点；'],
         total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。商品、活动、位置、服务这些核心维度不得合并或省略。',
         p0_items=[('活动规则虚标', '活动、折扣、会员权益只讲事实包确认的。'),
                   ('价格虚标', '价格、价差须与事实包一致，不虚标。'),
                   ('无依据口碑', '不宣称「最好、最便宜」等，只讲事实包提供的。'),
                   ('退款/售后承诺', '退换、售后政策未确认不提。')]),
    dict(code='education', name='教育培训', name_en='Education & Training',
         content=[content_cat('course', '课程 / 内容', 'Course', '课程内容、体系与教材'),
                  content_cat('teacher', '师资 / 资质', 'Teacher', '师资、资质、经验（仅已确认）'),
                  content_cat('class_size', '班型 / 形式', 'Class Size', '班型、上课形式与人数'),
                  content_cat('schedule', '上课安排 / 周期', 'Schedule', '上课安排、周期与有效期')],
         dims=['**价格算账**：团购价 vs 门店价、课时均价、省多少，单独成卖点；',
               '**课程内容**：课程体系、内容、教材，单独成卖点；',
               '**师资资质**：师资、资质、经验（仅已确认），单独成卖点；',
               '**班型形式**：班型、上课形式、人数，单独成卖点；',
               '**上课安排**：周期、时间、有效期，单独成卖点；',
               '**预约与使用**：需不需预约、有效期、试听安排，单独成卖点；',
               '**核销方式**：到店核销、附加费用（仅已确认），单独成卖点；',
               '**口碑与评价**：好评、评分、学员反馈，单独成卖点；',
               '**场景适配**：启蒙/升学/兴趣/职业，单独成卖点；'],
         total='事实丰富时生成 8~12 个卖点，每个卖点 5~8 个角度。课程、师资、班型、口碑这些核心维度不得合并或省略。',
         p0_items=[('效果承诺', '不宣称「包过、提分、保录取、包会」等效果承诺。'),
                   ('资质虚标', '办学资质、师资、认证只讲事实包确认的。'),
                   ('升学/就业保证', '不宣称「保送、包就业」等，未确认不提。'),
                   ('无依据口碑', '不宣称「最好、通过率第一」等，只讲事实包提供的。')]),
]

def gen_industry(ind):
    code = ind['code']
    d = os.path.join(HERE, code)
    os.makedirs(d, exist_ok=True)
    cats = make_categories(ind['content'])
    write_json(os.path.join(code, 'fact-categories.json'), {"schema_version": 1, "categories": cats})
    write_json(os.path.join(code, 'sides.json'), {"schema_version": 2, "sides": COMMON_SIDES, "category_sides": COMMON_CATEGORY_SIDES})
    write_md(os.path.join(code, 'selling-dimensions.md'), md(ind['name'], ind['dims'], ind['total']))
    write_md(os.path.join(code, 'p0-extra.md'), p0(ind['name'], ind['p0_items']))
    print('generated', code)

def build_catalog():
    nodes = [{"code": "general", "name": "综合/其他", "name_en": "General",
              "selling_dimensions": "", "sides": "", "fact_categories": "", "p0_extra": "", "children": []},
             {"code": "restaurant", "name": "餐饮（到店）", "name_en": "Restaurant (Dine-in)",
              "selling_dimensions": "restaurant/selling-dimensions.md",
              "sides": "restaurant/sides.json",
              "fact_categories": "restaurant/fact-categories.json",
              "p0_extra": "restaurant/p0-extra.md", "children": []}]
    for ind in INDUSTRIES:
        c = ind['code']
        nodes.append({"code": c, "name": ind['name'], "name_en": ind['name_en'],
                      "selling_dimensions": f"{c}/selling-dimensions.md",
                      "sides": f"{c}/sides.json",
                      "fact_categories": f"{c}/fact-categories.json",
                      "p0_extra": f"{c}/p0-extra.md", "children": []})
    catalog = {"schema_version": 2, "biz_domain": "local_service", "name": "本地生活", "name_en": "Local Services",
               "defaults": {"selling_dimensions": "", "sides": "", "fact_categories": "", "p0_extra": ""},
               "nodes": nodes}
    write_json('local-service.json', catalog)
    print('wrote local-service.json with', len(nodes), 'nodes')

if __name__ == '__main__':
    for ind in INDUSTRIES:
        gen_industry(ind)
    build_catalog()
