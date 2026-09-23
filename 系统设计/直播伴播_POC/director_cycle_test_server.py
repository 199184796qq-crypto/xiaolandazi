import json
import os
import re
import socket
import sys
import threading
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlparse

import requests
from qwen_tts_service import synthesize_wav

HOST = "127.0.0.1"
PORT = 8767
MODEL = "qwen3.8-flash"
API_URL = "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"
CORE_BASE = "http://127.0.0.1:8081"
CORE_TOKEN = os.getenv("CORE_INTERNAL_TOKEN", "local-core-dev-token")
HTML_PATH = Path(__file__).with_name("director_cycle_test.html")
STRATEGY_CHAT_HTML_PATH = Path(__file__).with_name("strategy_chat_test.html")
TIMELINE_TEST_DIR = Path(r"E:\直播伴播\测试素材\母带时间轴测试")
POLICY_DIR = Path(__file__).with_name("policies")
CONFIG_KEY = "livecompanion:poc:director_config:9"
STRATEGY_CHAT_CONFIG_KEY = "livecompanion:poc:strategy_chat:9:config"
STRATEGY_CHAT_HISTORY_KEY = "livecompanion:poc:strategy_chat:9:history"
TRACE_TTL_SECONDS = 7 * 24 * 3600
INPUT_PRICE_PER_M = 0.8
OUTPUT_PRICE_PER_M = 2.7

DEFAULT_CONFIG = {
    "room_id": "9",
    "customer_name": "杨博士官方旗舰店",
    "room_name": "杨博士官方旗舰店 · 童子鸡直播间",
    "product_name": "1号童子鸡 / 原切童子鸡 / 14号小母鸡",
    "persona": "自然、接地气、像真人直播场控；主线要有承接、有解释、有变化，不要机械念参数；不乱用宝子、家人们、姐、哥等称呼；插话回答尽量短。",
    "tts_voice": "Cherry",
    "tts_speech_rate": 1.08,
    "speech_plan_template": '【完整真人口播方案模板｜先完整播，不考虑打断】\n\n目标：\n不是“逐条念参数”，而是像真人主播一样围绕一个商品持续讲一整轮。整轮要有开头、有展开、有选择引导、有收口；中间可以重复核心商品和链接，但不能重复同一套句子。\n\n第一段｜把人拉进当前商品\n- 直接告诉大家现在重点讲什么，不要每次都说“刚进来的朋友”。\n- 可以用：提问、提醒、选择题、观众常见疑问来起头。\n- 1~3句内进入商品，不长时间空喊。\n\n第二段｜主商品完整塑品\n- 先讲产品身份：它到底是什么。\n- 再讲最容易搞混的基础事实：生熟、月龄、公母、规格、重量等。\n- 不要“参数1、参数2、参数3”机械罗列，要像在给人解释。\n- 同一核心事实允许重复一次，但第二次必须换说法、换角度。\n- 每讲完一个小点，要自然接下一个小点，不要说“后面再讲”“下一段再讲”。\n\n第三段｜把产品讲深\n- 有产地、养殖、制作工艺、口感、包装、宰杀、配料等真实资料时，在这里连续展开。\n- 当前事实库没有的内容一个都不能编。\n- 没有足够素材时宁可少讲，也不要用空话硬撑长度。\n\n第四段｜原切 / 规格 / 变体说明\n- 解释“原切是什么、和整只有什么关系、能怎么做”。\n- 站在消费者容易误解的地方讲清楚，而不是念定义。\n- 如果有做法教程、客服入口等真实信息，自然带出来。\n\n第五段｜第二商品 / 其他链接\n- 自然带出14号小母鸡等其他选择。\n- 先讲“它是什么”，再讲真实规格。\n- 只做事实上的区别，不能凭空说哪个好吃、哪个更嫩、哪个更适合谁。\n\n第六段｜链接与选择动作\n- 把1号、14号等链接重新对上。\n- 可以重复“1号是童子鸡、14号是小母鸡”，但句式要变化。\n- 有价格、券、库存、平台补贴、赠品、快递、活动时间等实时事实时，这里可以明显加强成交动作。\n- 没有这些实时事实时，只说“看对应链接、确认规格、按需求选择”，绝不能编“最后几张券、手慢无、恢复原价”。\n\n第七段｜完整收口\n- 把这一轮最重要的2~4个信息再压缩一次。\n- 给一个明确动作：看1号 / 看14号 / 先确认规格 / 评论区提问。\n- 这一轮到这里要完整结束，不留下“后面再详细讲”这种半句话。\n\n语言节奏：\n- 像真人直播，不像播音稿。\n- 允许“咱们、宝宝们、你看、这个地方、很多人会问”等口语，但不要形成固定口头禅。\n- 禁止连续重复“刚进来的朋友”“把镜头拉回来”“插曲结束”“回到正题”“咱们接着聊”。\n- 一个完整塑品大段允许约320~520字；如果真实素材很多，可以更长。\n- 核心原则：重复信息，不重复句子；主线不断，话题不乱跳；具体事实全部来自事实库。',
    "reference_script": '【真人直播样本｜只学表达结构和节奏，不自动当成当前商品事实】\n这种跑山鸡呢，运动量特别大，所以你们收到货呢，你们会发现这个土鸡的鸡皮特别薄，皮下都是精瘦肉，肉质非常紧实。然后直播中有人问价格，主播会先顺手回答，再马上回到商品主线。接着继续讲散养、活动量、鸡皮、肉质、现杀、鸡胗鸡肝鸡心、独立包装、物流、做法等，一口气把产品故事讲深，再自然回到下单动作。\n\n学习重点：\n1. 商品是主角，链接只是动作，不要一直干巴巴报1号、14号。\n2. 先讲“为什么值得听”，再连续讲产品身份、来源/养殖/工艺、肉质/状态、宰杀/包装、做法等真实素材。\n3. 一段里面允许出现自然口语和解释，但不能像参数表。\n4. 有观众问题时可以顺手回答，回答完直接接回刚才商品内容。\n5. 样本中的皖南、高山散养、不农药、不圈养、鸡皮薄、精瘦肉、活鸡现杀、鸡胗鸡肝鸡心、顺丰等，只是别人的商品事实。当前商品只有在事实库明确提供时才能说。',
    "deep_facts_text": 'origin=\nbreed=\nraising_method=\nraising_environment=\nmovement=\nfeeding=\nskin_meat=\nslaughter=\noffal=\npackaging=\ncourier=\ncooking_recommendation=',
    "facts_text": """link_1_product=1号链接是童子鸡
link_1_state=1号童子鸡是生的、生鲜，回家需要自己烹饪
link_1_age_sex=1号童子鸡是4到5个月、没有开叫的子公鸡
link_1_gross_weight=1号童子鸡单只毛重2斤以上
link_1_net_weight=1号童子鸡单只净重1.2斤-1.8斤左右
original_cut_definition=原切鸡是整只童子鸡去头、去鸡屁股后切块
original_cut_quality=原切鸡品质和整只的一样
original_cut_steam=原切鸡切块也可以清蒸
link_14_product=14号链接是小母鸡
link_14_age=14号小母鸡4-5个月大
link_14_gross_weight=14号小母鸡单只毛重2斤及以上
link_14_net_weight=14号小母鸡单只净重1.3斤及以上
delivery_jzh=江浙沪皖正常发货后基本隔天到
delivery_other=其他地区正常发货后约2-3天
cooking_help=详细做法可联系客服，或查看包装袋后面的教程入口""",
    "answer_rules": """直播主线节奏最高优先级，但正常观众问题尽量给有用回答。
普通进房不逐个欢迎，高频进房合并欢迎。
塑品、讲链接阶段可以在完整句/完整段落边界软打断高价值问题，回答完自然回主线。
已确认商品事实必须以事实库为准，不能编造价格、库存、优惠、赠品、快递品牌等具体信息。
事实库没有直接答案时，可以结合已确认资料和一般常识做非绝对解释、烹饪建议或经验判断，但要和“已确认商品事实”区分开。
价格、库存、补贴、赠品、具体快递、当天发货等动态信息不能猜具体值，但仍要回应，并告诉观众以当前商品页、结算页或客服确认。
普通食用、烹饪、老人小孩等问题不要条件反射式推给医生；只有明确过敏、疾病、用药、症状、治疗或诊断问题才提高谨慎程度。
禁止百分百、绝对、保证、一定、最好、最香、最嫩、完全没问题等绝对化或保证式表达。
同类问题短时间重复出现只回答一次。
软打断回答后必须回到原来的直播阶段继续。
用户名隐藏时不猜名字、性别或年龄。""",
    "durations": {
        "OPENING": 60,
        "PRODUCT_SHAPING": 240,
        "LINK_GUIDE": 90,
        "CONVERSION": 120,
        "QNA_WINDOW": 60
    },
    "speak_every": {
        "OPENING": 0.6,
        "PRODUCT_SHAPING": 0.4,
        "LINK_GUIDE": 0.5,
        "CONVERSION": 0.7,
        "QNA_WINDOW": 0.6
    }
}

DEFAULT_STRATEGY_CHAT_CONFIG = {
    "industry_path": "食品 > 生鲜 > 禽类",
    "active_layer": "L3",
    "l1_policy": """基础合规策略（测试版）
1. 不编造商品事实、价格、库存、优惠、赠品、物流品牌等信息。
2. 不使用百分百、绝对、保证、一定、最好、最香、最嫩、完全没问题等绝对化或保证式表达。
3. 涉及过敏、疾病、用药、症状、治疗、诊断等真正高风险问题时，提高谨慎等级。
4. 普通食用、做法、老人小孩等问题，不因为出现敏感词就机械推诿。
5. 上层合规规则不可被行业层或用户层解除；冲突时以上层为准。
6. 当前为测试策略，不替代正式法务和平台规则库。""",
    "l2_policy": """食品 > 生鲜 > 禽类 行业默认策略
1. 正常商品问题尽量回答，不因为资料库没有直接命中就沉默。
2. 产地、养殖、饲喂、肉质、规格、生熟、原切、宰杀、内脏等问题优先使用已确认商品事实。
3. 做法问题允许给清蒸、炖、煮、红烧等一般烹饪建议，但不推导更香、更嫩、营养更高、不柴等未确认结果。
4. 库存、价格、包邮、优惠、当天发货等实时信息不猜具体值，要告诉用户如何查看当前状态。
5. 老人、小孩、孕妇普通食用问题先正常回答并给低风险建议，不自动转医生。
6. 回答要短、自然、像主播顺手接话；回答完自然回主线，不说“回到正题、刚才说到、插曲结束”。
7. 没有直接事实时，可以结合已确认资料和一般常识做非绝对解释。""",
    "l3_policy": """当前用户特殊策略
1. 本店把养殖方式表述为“不圈养、跑山鸡、溜达鸡”，不要擅自改成“散养”。
2. 没有实时库存数据时不报具体数量。
3. 普通问题不要机械说“咨询客服”或“咨询医生”。
4. 回答尽量直接、有用，少说系统式免责声明。""",
    "pending_update": None,
    "updated_at": "",
}

STAGES = {
    "OPENING": {
        "label": "开场/回流",
        "next": "PRODUCT_SHAPING",
        "interruptible": True,
        "segments": [
            {"goal": "欢迎刚进来的观众，但不要逐个点名；快速把注意力带回今天的童子鸡主题。", "facts": ["link_1_product", "link_14_product"]},
            {"goal": "告诉观众今天直播间主要看1号童子鸡和14号小母鸡，先把两个产品定位说清楚。", "facts": ["link_1_product", "link_14_product"]},
            {"goal": "给观众一个非常简短的观看指引：先听产品区别，再看适合自己的链接；这一段不要再次欢迎新进观众。", "facts": ["link_1_product", "link_14_product"]},
            {"goal": "自然从开场过渡到产品塑造，准备开始讲1号童子鸡的基础信息。", "facts": ["link_1_product"]}
        ]
    },
    "PRODUCT_SHAPING": {
        "label": "塑品",
        "next": "LINK_GUIDE",
        "interruptible": True,
        "segments": [
            {
                "goal": "这一轮塑品必须一次讲完整，不拆成多个小段。把1号童子鸡、原切童子鸡、14号小母鸡的已知信息组织成一整段真人直播口播：先把直播间今天主要卖什么说清楚，再完整讲1号童子鸡的生鲜状态、月龄公母、毛重净重；然后自然解释原切是什么、和整只品质一致、切块也可以清蒸、具体做法可联系客服或看包装教程；最后带出14号小母鸡的月龄和重量，帮观众听懂1号、原切、14号分别是什么。整段必须有开头、有展开、有收束，不能讲到一半用“后面再细讲、接下来再说、咱们往下聊”来结束。只使用事实库内容，不编价格、口感、工艺、优惠、库存、快递或任何原因。",
                "facts": [
                    "link_1_product",
                    "link_1_state",
                    "link_1_age_sex",
                    "link_1_gross_weight",
                    "link_1_net_weight",
                    "original_cut_definition",
                    "original_cut_quality",
                    "original_cut_steam",
                    "cooking_help",
                    "link_14_product",
                    "link_14_age",
                    "link_14_gross_weight",
                    "link_14_net_weight"
                ]
            }
        ]
    },
    "LINK_GUIDE": {
        "label": "讲链接",
        "next": "CONVERSION",
        "interruptible": True,
        "segments": [
            {"goal": "明确告诉观众1号链接是什么产品。", "facts": ["link_1_product"]},
            {"goal": "把1号链接最容易被问的重量再带一次，帮助观众下单前确认。", "facts": ["link_1_gross_weight", "link_1_net_weight"]},
            {"goal": "明确告诉观众14号链接是什么产品。", "facts": ["link_14_product"]},
            {"goal": "把14号链接的小母鸡月龄和重量带一次。", "facts": ["link_14_age", "link_14_gross_weight", "link_14_net_weight"]},
            {"goal": "用一句对比帮助观众分清1号和14号，避免讲价格和库存。", "facts": ["link_1_product", "link_14_product"]},
            {"goal": "自然收口链接讲解，准备进入转化阶段。", "facts": ["link_1_product", "link_14_product"]}
        ]
    },
    "CONVERSION": {
        "label": "逼单/转化",
        "next": "QNA_WINDOW",
        "interruptible": False,
        "segments": [
            {"goal": "做第一轮温和转化：需要童子鸡的看1号，需要小母鸡的看14号；不编造价格库存。", "facts": ["link_1_product", "link_14_product"]},
            {"goal": "从选择清晰的角度继续转化，帮观众确认自己要看哪一个链接。", "facts": ["link_1_product", "link_14_product"]},
            {"goal": "用重量事实帮助还在犹豫的观众确认1号童子鸡份量，但不制造虚假紧迫感。", "facts": ["link_1_gross_weight", "link_1_net_weight"]},
            {"goal": "用14号小母鸡的月龄和重量做一次选择提示，不夸大差异。", "facts": ["link_14_age", "link_14_gross_weight", "link_14_net_weight"]},
            {"goal": "再次明确1号和14号分别是什么，做第二轮下单指引。", "facts": ["link_1_product", "link_14_product"]},
            {"goal": "完成这一轮转化收口，提示接下来会进入短问答窗口。", "facts": ["link_1_product", "link_14_product"]}
        ]
    },
    "QNA_WINDOW": {
        "label": "问答窗口",
        "next": "OPENING",
        "interruptible": True,
        "segments": [
            {"goal": "告诉观众现在进入短问答窗口，可以问童子鸡重量、生熟、原切区别、到货时效等问题。", "facts": ["link_1_gross_weight", "link_1_state", "original_cut_definition", "delivery_jzh", "delivery_other"]},
            {"goal": "顺手回答直播间高频事实：1号童子鸡重量。", "facts": ["link_1_gross_weight", "link_1_net_weight"]},
            {"goal": "顺手回答直播间高频事实：原切童子鸡是什么。", "facts": ["original_cut_definition", "original_cut_quality"]},
            {"goal": "结束问答窗口，告诉观众接下来重新从开场和产品重点讲一轮。", "facts": ["link_1_product", "link_14_product"]}
        ]
    }
}

FULL_SPEECH_PLAN_SYSTEM = """你是一名正在直播间现场带货的真人主播。

你只做一件事：围绕当前主商品连续讲完整一轮。
你不知道“第1段、第2段……第7段”这件事，也不要按章节、阶段、模块组织语言。系统会在你输出后自行切成播放块，这和你无关。

只允许输出严格 JSON：
{"text":"这里是一整篇可以直接念出来的连续直播口播"}

【核心要求】
- 整篇听起来必须是同一个主播、同一个话题、同一口气持续往下讲。
- 商品本身是主角。只要还有真实商品素材，就继续围绕当前商品往深处讲。
- 不要为了完成结构强制换商品，不要为了完成结构强制报链接，不要做“总结报告”。
- reference_script 只学习真人主播的语气、节奏、承接和连续塑品方式；其中任何商品事实都不能自动迁移。
- speech_plan_template 只作为“怎么说”的风格规则，不要把其中的章节、编号或阶段感带进最终口播。

【真人连续塑品】
有真实素材时，优先自然地从这些方向连续往下讲：
商品身份 → 产地/来源 → 品种 → 养殖方式 → 生长环境 → 活动量 → 饲喂 → 外观/鸡皮/肉质 → 宰杀状态 → 内脏保留 → 包装/物流 → 做法 → 最后自然带一下购买动作。
这只是思考顺序，不是输出提纲。不要让观众听出你在按列表念。

每讲一个真实点，可以顺手解释一下、换一种口语再说一遍、自然带到下一个真实点。
允许“我跟大家讲一下啊、你看我们这个、这个地方大家注意一下、很多人会问、你们收到货以后、而且我跟你讲”等真人口语，但不要固定复读同一句。

【主商品优先】
- 整篇约80%的内容讲当前主商品本身。
- 第二商品如果必须提到，最多自然带1~2句，不要因此切换成新的介绍频道。
- “1号、14号、链接、下单”只在确实需要做选择动作时出现，整篇不要反复报。
- 不要使用“第一、第二、第三”“总结一下”“最后总结”“下面讲”“接下来讲”这类报告式结构词。
- 不要说“刚进来的朋友、把镜头拉回来、回到正题、插曲结束、咱们接着聊”。

【事实边界与合理推理】
所有具体商品事实只能来自 allowed_facts。
严禁把 reference_script 里的事实当成 allowed_facts。
允许基于多个已知事实做自然、克制的直播式推理和承接，但必须用弱判断、非绝对语气，例如“你看这个状态”“从这些信息看”“整体会给人一种……的感觉”“相对来说”“可以理解为”。
允许把两个已经分别成立的事实自然连起来说，但不得把相关性说成确定因果。
禁止用“决定了、必然、一定、绝对、肯定、百分百、保证”等强因果或绝对结论。
以下推理仍然禁止：
- 月龄/公母不能自行推出肉嫩、口感、用途、成熟度；
- 运动量不能自行推出肉质结论，除非 allowed_facts 明确给出；
- 毛重/净重不能自行解释差额原因或可食重量；
- 生鲜不能自行推出冻品、冷链、保鲜方式；
- 活鸡现杀不能自行推出“别家不新鲜”等比较；
- 内脏保留不能自行编“别家会去掉、压秤”等原因；
- 不得增加价格、库存、优惠券、补贴、赠品、快递公司、认证、营养/保健效果。
没有事实就跳过，宁可少讲，绝对不要编。

【广告合规语言】
- 直播推销语境中禁止使用商品绝对化、最高级或无法证明的唯一性表达。
- 明确禁止：“国家级”“最高级”“最佳”等法定典型绝对化用语。
- 同时规避常见高风险表达：全网第一、行业第一、唯一、顶级、极品、史上最好、全网最低、最低价、百分百、100%、绝对、保证、零风险、永久、无敌、最牛、最强、最划算、闭眼买、绝对不会、肯定不会。
- “最”字不是机械一律删除：像“大家最关心的问题”这种不用于宣称商品最高等级的普通口语可以保留；但“最好的鸡、最嫩、最好吃、最低价”等商品绝对化宣称不允许。
- 不得贬低或虚构其他商家、同类商品来抬高当前商品。
- 不得虚构观众实时行为来制造真实感，例如“后台私信很多”“刚有人问”“评论区都在问”；可以说“很多人会关心/常见的问题是”，但不要伪装成刚发生的互动。
- 避免“这个细节骗不了人”“这点大家放心”“全部都是”“完全没问题”等过强口语；如果事实成立，改成“可以看一下/目前信息是/按实际收到状态看”等中性表达。

【语言形态】
- 只输出主播最终要说的话，放在 JSON 的 text 字段。
- 不要标题、编号、列表、解释、分析。
- 不要像商品详情页，不要像参数说明书，不要像客服回答集合。
- 不要过早收尾；有足够真实素材时建议650~1000个中文字符，素材少时可以短。
【稳定骨架 + 动态表达】
- 每一轮的大框架保持相近：商品身份 → 来源/养殖 → 活动/饲喂 → 外观/肉质 → 宰杀/内脏 → 规格/原切 → 物流/做法 → 自然成交；缺少事实的环节直接跳过。
- variation_profile 是本轮的“变化引擎”：它决定开头方式、重点素材、句子节奏、承接方式和收尾方式。
- focus_groups 本轮要多展开1~3句；其他真实信息仍要覆盖，但可以简洁带过。
- recent_rounds 是“不要照着说”的负参考。不得连续多轮使用相同开头、相同承接句、相同收尾句。
- 核心事实允许重复；表达方式不要重复。不要为了变化而打乱商品主线。
- 不要逐字复刻最近几轮中的长句。相同事实必须尽量换句式、换角度、换前后承接。
- 目标效果：观众听得出一直在讲同一套商品逻辑，但听不出在循环播放同一篇稿子。

- 核心原则：商品主线不断；重复信息但不重复句子；链接只是动作，不是内容。"""

FULL_SPEECH_CONTINUOUS_GUARD_SYSTEM = """你是直播口播事实守门员兼自然语言编辑。

输入：
- allowed_facts：唯一允许出现的具体商品事实；
- draft_text：已经写好的真人直播口播。

任务：
在尽量保持 draft_text 真人主播节奏、口语感、连续性的前提下进行“轻量合规修正”。允许基于 allowed_facts 做自然、克制、非绝对的合理推理；只有明显越界的新事实、强因果、绝对化宣称、无法证明的承诺和违规广告表达需要删除或改写。不要因为一句话有一点推理就整句删掉。

只输出严格 JSON：
{"text":"清洗后的完整连续口播"}

必须执行：
- 不增加新的可核查商品事实，但允许对已知事实做弱推理、生活化解释和自然承接。
- 弱推理可以使用“相对来说、从这些信息看、你看这个状态、整体给人的感觉、可以理解为”等表达；不得使用“决定了、必然、一定、绝对、肯定、百分百、保证”等确定性因果。
- reference 或常识都不能作为事实来源，只有 allowed_facts 可以。
- 可以保留纯口语过渡，例如“我跟大家讲一下啊”“你看”“这个地方注意一下”，只要不制造新事实。
- 不要把清洗后的内容变回参数表；仍然是一整篇真人口播。
- 清洗不是“删短”。一句话里如果只有半句越界，要只删越界半句，把其中 allowed_facts 支持的内容保留下来。
- 如果 allowed_facts 中明确有 origin、breed、raising_method、raising_environment、movement、feeding、skin_meat、slaughter、offal 等深度塑品素材，应尽量每类至少自然保留一次，不要因为同一句里混入了一个越界修饰就把整条真实素材删掉。
- 当前 allowed_facts 足够丰富时，清洗后仍应保持约500~900字的连续口播；除非真实素材确实不足，不要压缩成短摘要。

以下类型如果 allowed_facts 没明确写，必须删除或弱化；如果只是合理承接，可以改成非绝对表达：
1. 与超市鸡、普通鸡、圈养鸡、其他商家做比较；
2. “不是冻品/冷冻货/解冻货/预制”等状态；
3. 由运动量推出低脂、回弹、弹性、口感、肉质等因果；
4. 由月龄、公母推出嫩、柴、细腻、风味、适合老人小孩等；
5. 由毛重净重解释去毛去脏、损耗、可食重量；
6. “偷偷去内脏、压秤、别家不保留”等对其他商家的描述；
7. 没提供的烹饪效果，如鲜甜、入味、炖汤更好、怎么做都好吃；
8. 没提供的客服能力，如一对一指导、火候指导；
9. 没提供的库存、价格、优惠、券、赠品、补贴、限时、催单；
10. 没提供的物流与新鲜度强因果，例如“隔天到所以一定更新鲜”；可以改成“正常时效是……，大家收到后按实际状态检查”。

【广告合规】
- 必须删除或改写商品绝对化/最高级/唯一性宣称：“国家级”“最高级”“最佳”“全网第一”“行业第一”“唯一”“顶级”“极品”“史上最好”“全网最低”“最低价”“百分百”“100%”“绝对”“保证”“零风险”“永久”“无敌”“最牛”“最强”“最划算”等。
- “大家最关心”“最容易搞混”这类不用于宣称商品最高等级的普通口语可保留。
- 不得使用无法证明的效果承诺、结果保证、贬低同行或虚构对比。

特别注意：
- raising_method=不圈养，只能说不圈养/跑山/溜达，不能扩写成“绝不吃饲料”“比圈养鸡更好”。
- skin_meat 如果写了鸡皮薄、精瘦肉、肉质紧实，可以直接说这些，但不能再创造“按压回弹、没有一点肥肉”等新表现，除非事实里明确写。
- slaughter=活鸡新鲜宰杀，只能说活鸡新鲜宰杀，不能改成“不是冻品”。
- offal=鸡胗鸡肝鸡心保留，只能说保留，不得解释别家为什么不保留。
- cooking_help 只支持“联系客服/看包装教程入口”时，不得扩写成一对一指导或具体菜式。

输出必须仍然从头到尾是同一个商品主线，不要新增总结、编号、列表。"""

FULL_SPEECH_PLAN_GUARD_SYSTEM = """你是直播完整口播的事实守门员。

输入包含 allowed_facts 和 plan_json。你必须返回严格 JSON，仍然保持7段结构。

规则：
- 必须保留正好7段、section 1到7顺序不变。
- 删除或改写任何 allowed_facts 不能直接支持的具体商品事实。
- 禁止增加价格、券、库存、补贴、赠品、快递、产地、养殖、工艺、口感、包装、宰杀、营养、适用人群等未提供事实。
- 禁止解释毛重和净重差额的原因；禁止出现“去头去爪后的净重、去掉内脏后的净重、处理后净重”等说法，除非事实库明确提供；禁止由月龄推导口感/肉质/用途/成熟度；禁止拿子公鸡和老母鸡做事实库没有的比较；禁止由生鲜推导保鲜方式；14号没有生熟事实时不得说14号是生的或生鲜。
- 可以保留自然口语、称呼、反问、过渡，只要不形成新事实。
- 7段要保持连续自然，不要改成说明书式参数罗列。
- 不要输出 Markdown，不要解释修改过程。"""

CONTROL_SYSTEM = """你是直播间场控 Control。你的目标不是“念参数”，而是像真人主播一样持续带节奏、塑品、引导选择。

【最重要：学习真人直播的组织方式，不照抄别人的句子】
一段好的直播口播通常不是一条直线，而是一个小循环：
1. 把注意力拉回来：一句动作指令、问题、提醒或承接；
2. 说清当前产品/链接到底是什么；
3. 用 2~4 个已经确认的事实连续塑品，不要参数报表式罗列；
4. 从观众视角解释“这意味着什么/怎么理解”，但只能做语言解释，不能增加新商品事实；
5. 自然带到链接、选择、做法、发货等下一个已知事实；
6. 再回到一个轻量 CTA 或选择提醒，形成一个小闭环。
长段里允许有 2~4 个这样的小波次，让听感像真人一直在带直播，而不是“讲完一个点就停”。

【有意识的重复，不是复读】
- 真人直播会反复说核心链接、核心产品和核心购买动作，这是允许的。
- 但同一个核心事实重复时，必须换切入角度、句式和位置；不能连续两段用同一个开头、同一个收尾。
- recent_speech 是最近已经播过的话，必须主动避开重复表达。
- 除 OPENING 开场欢迎或 MEMBER 聚合欢迎外，不要反复说“刚进来的朋友/新进来的朋友/刚刷进来的朋友”。
- RESUME 恢复主线时优先直接续内容，不要反复说“插曲结束/翻篇/回到正题/咱们接着聊”。

【动态信息是直播高转化的关键，但必须真实】
订单数、优惠券剩余、价格、平台补贴、库存、倒计时、赠品、快递、活动失效时间等，只有 allowed_facts 明确提供当前值时才能说。
如果没有动态事实，不允许为了制造直播感编造“最后几十张券、还有多少单、手慢无、恢复原价、平台加补、发某快递”等内容。

【事实锁定】
- 所有关于商品本身的具体陈述，只能来自 allowed_facts。
- 允许同义改写、调整顺序、用反问和生活化连接，但不能增加新的事实或原因。
- 禁止由月龄推导口感、成熟度、纤维、营养；禁止由重量推导包装、冰袋、损耗、去内脏或任何处理原因；禁止把“毛重/净重”解释成“处理前/处理后”；禁止由生鲜推导保鲜工艺。
- 禁止添加 allowed_facts 没有的口感、品质、产地、工艺、营养、适用人群、物流原因、价格、优惠、库存、赠品、稀缺性。
- 不解释系统规则，不输出 JSON，只输出最终要播的话。

【口播方案模板】
- 当 phase=PRODUCT_SHAPING 时，必须优先遵循输入中的 speech_plan_template。它决定这一整轮怎么组织；allowed_facts 决定哪些具体事实能说。
- speech_plan_template 只决定结构和语言动作，绝不能覆盖事实锁定规则。

【阶段长度】
- PRODUCT_SHAPING 塑品：一次生成一个完整大段，约 320~520 个中文字符，允许 10~18 句。要完整形成“抓注意力→核心产品→事实塑品→选择理解→原切/其他产品→收束动作”的一整段，长一点没关系，完整优先。
- OPENING 开场：约 80~140 字。
- LINK_GUIDE 讲链接：约 100~180 字。
- CONVERSION 转化：约 100~180 字；有动态价格/券/库存事实时可以明显更强，没有就只做真实的选择引导。
- QNA_WINDOW 问答窗口：约 70~120 字。
- INTERRUPT 插话：1~2 句，短、准。
- RESUME 恢复：1~2 句，直接续内容，不复述整段。"""

FACT_GUARD_SYSTEM = """你是直播话术的事实守门员。输入会给你 draft 和 allowed_facts。

任务：把 draft 改成仍然自然、像真人直播的话术，但删除一切 allowed_facts 不能直接支持的商品事实。

严格规则：
- 任何具体商品事实必须能从 allowed_facts 直接推出；不能靠常识补充。
- 禁止自行解释“为什么”，禁止推导口感、肉质、成熟度、品质、营养、适合做法、包装、冰袋、损耗、去内脏、任何重量差额原因、物流原因、产地、价格、优惠、库存、赠品。除非 allowed_facts 明确说原切属于某个链接，否则禁止说“1号里的原切选项”或类似链接归属。
- 数字、月龄、重量、产品身份、生熟状态等必须与 allowed_facts 一致。
- 可以保留纯口语连接、称呼、反问、过渡，只要它们不形成新的商品事实。
- 如果 draft 某一句包含一半正确一半编造，只保留有事实依据的部分。
- 不要解释你改了什么，不输出 JSON，只输出最终可播话术。
- 宁可变短，也不能留下一条没有依据的商品陈述。
- recent_speech 里如果已经连续出现同一类开头或过渡语，要把 draft 的重复开头改掉。尤其不要重复“刚进来的朋友”以及“插曲结束/翻篇/回到正题/咱们接着聊”这一类元话术。"""

COOLDOWN_LOCK = threading.Lock()
LAST_LIVE_INTENT = {}
LAST_MEMBER_WELCOME = 0.0

def get_api_key():
    value = os.getenv("DASHSCOPE_API_KEY")
    if value:
        return value
    if sys.platform == "win32":
        import winreg
        try:
            with winreg.OpenKey(winreg.HKEY_CURRENT_USER, r"Environment") as key:
                value, _ = winreg.QueryValueEx(key, "DASHSCOPE_API_KEY")
                return value
        except OSError:
            pass
    raise RuntimeError("DASHSCOPE_API_KEY 未配置")

def redis_encode(parts):
    out = "*" + str(len(parts)) + "\r\n"
    for part in parts:
        raw = str(part).encode("utf-8")
        out += "$" + str(len(raw)) + "\r\n" + raw.decode("utf-8") + "\r\n"
    return out.encode("utf-8")

def redis_read(fp):
    prefix = fp.read(1)
    if not prefix:
        raise RuntimeError("Redis connection closed")
    line = fp.readline().rstrip(b"\r\n")
    if prefix == b"+":
        return line.decode("utf-8")
    if prefix == b"-":
        raise RuntimeError("Redis: " + line.decode("utf-8", "replace"))
    if prefix == b":":
        return int(line)
    if prefix == b"$":
        size = int(line)
        if size < 0:
            return None
        data = fp.read(size)
        fp.read(2)
        return data.decode("utf-8")
    if prefix == b"*":
        count = int(line)
        return [redis_read(fp) for _ in range(count)]
    raise RuntimeError("Unsupported Redis response")

def redis_cmd(*parts):
    with socket.create_connection(("127.0.0.1", 6379), timeout=2) as sock:
        sock.sendall(redis_encode(parts))
        return redis_read(sock.makefile("rb"))

def load_config():
    config = json.loads(json.dumps(DEFAULT_CONFIG, ensure_ascii=False))
    try:
        raw = redis_cmd("GET", CONFIG_KEY)
        if raw:
            saved = json.loads(raw)
            config.update(saved)
            config["durations"] = {**DEFAULT_CONFIG["durations"], **saved.get("durations", {})}
            config["speak_every"] = {**DEFAULT_CONFIG["speak_every"], **saved.get("speak_every", {})}
    except Exception:
        pass
    return config

def save_config(config):
    redis_cmd("SET", CONFIG_KEY, json.dumps(config, ensure_ascii=False))


def load_strategy_chat_config():
    config = json.loads(json.dumps(DEFAULT_STRATEGY_CHAT_CONFIG, ensure_ascii=False))
    try:
        raw = redis_cmd("GET", STRATEGY_CHAT_CONFIG_KEY)
        if raw:
            saved = json.loads(raw)
            if isinstance(saved, dict):
                config.update(saved)
    except Exception:
        pass
    if config.get("active_layer") not in ("L1", "L2", "L3"):
        config["active_layer"] = "L3"
    return config


def save_strategy_chat_config(config):
    clean = json.loads(json.dumps(config, ensure_ascii=False))
    clean["updated_at"] = time.strftime("%Y-%m-%dT%H:%M:%S")
    redis_cmd(
        "SET",
        STRATEGY_CHAT_CONFIG_KEY,
        json.dumps(clean, ensure_ascii=False),
    )
    return clean


def load_strategy_chat_history(limit=30):
    try:
        rows = redis_cmd("LRANGE", STRATEGY_CHAT_HISTORY_KEY, "0", str(max(limit - 1, 0))) or []
        out = []
        for row in reversed(rows):
            try:
                item = json.loads(row)
                if isinstance(item, dict):
                    out.append(item)
            except Exception:
                pass
        return out
    except Exception:
        return []


def append_strategy_chat_history(role, content, meta=None):
    item = {
        "role": str(role),
        "content": str(content or "").strip(),
        "created_at": time.strftime("%Y-%m-%dT%H:%M:%S"),
    }
    if meta:
        item["meta"] = meta
    try:
        redis_cmd(
            "LPUSH",
            STRATEGY_CHAT_HISTORY_KEY,
            json.dumps(item, ensure_ascii=False),
        )
        redis_cmd("LTRIM", STRATEGY_CHAT_HISTORY_KEY, "0", "39")
        redis_cmd("EXPIRE", STRATEGY_CHAT_HISTORY_KEY, str(7 * 24 * 3600))
    except Exception:
        pass
    return item


def build_effective_strategy(config):
    return """【策略合并规则】
1. L1 基础策略为强制边界，L2/L3 不能解除 L1。
2. L2 是行业默认策略。
3. L3 是当前客户特殊策略；与 L2 冲突时，L3 可以替换或覆盖 L2，但仍受 L1 约束。
4. 没有冲突的 L2 与 L3 同时生效。

【L1 基础策略】
{l1}

【L2 行业策略｜{industry}】
{l2}

【L3 用户特殊策略】
{l3}
""".format(
        l1=str(config.get("l1_policy") or "").strip(),
        industry=str(config.get("industry_path") or "未选择"),
        l2=str(config.get("l2_policy") or "").strip(),
        l3=str(config.get("l3_policy") or "").strip(),
    ).strip()


STRATEGY_CONFIG_AGENT_SYSTEM = """你是“直播伴播策略配置 Agent”。
你通过自然语言和用户交流，把用户的要求整理成可执行的策略，但不要让用户学习 DSL、JSON 或 Prompt。

系统有三层策略：
L1：基础合规/平台规则。正式产品中只有有权限的管理人员可以修改。L1 是强制边界，L2/L3 不能解除。
L2：行业默认策略。正式产品中由有权限的运营人员维护。L2 给某行业提供默认能力。
L3：用户特殊策略。客户可以通过自然语言补充、替换或关闭 L2 的业务规则，但不能突破 L1。

当前处于测试阶段，用户可以把 active_layer 切到 L1/L2/L3 来测试三层编辑能力。

你的工作方式：
1. 先理解用户是在解释、询问，还是希望修改当前 active_layer。
2. 如果用户希望修改，返回一份“完整的新版本策略文本”，而不是只返回差异。
3. 策略文本必须是普通中文编号规则，让非技术人员也能读懂；不要 YAML、不要 JSON DSL、不要 policy_id。
4. L3 应重点记录“这个客户和行业默认不一样的地方”，不要无意义复制整份 L2。
5. 如果 L3 请求和 L1 冲突，不要生成突破 L1 的规则；在 conflicts 中说明。
6. 如果用户只是讨论或问“这样行不行”，可以 action=EXPLAIN，不强行修改。
7. 用户给一整套主播话术时，优先提炼：事实表达习惯、回答习惯、特殊禁语、实时信息处理、特殊业务流程；不要把整篇话术原样塞进策略。
8. 说人话。assistant_message 要像一个懂直播业务的配置助手，说明你理解了什么、准备怎么改。
9. 用户提供“话术/示例说法”时，必须判断表达模式：
   - 用户明确说“就按这句话、必须原话、一字不改、固定这样说、100%原话”等，记录为“表达方式：固定原话”。这种模式运行时必须逐字使用，不能交给模型改写。
   - 用户说“按这个意思、类似这样、灵活说、换着说、不要每次一样”等，记录为“表达方式：按意思灵活生成”。这种模式只锁定意思、事实和边界，允许运行时变换句式、开头、解释角度和承接。
   - 用户没有明确要求逐字固定时，默认使用“按意思灵活生成”。
10. “固定原话”如果与 L1 冲突，不允许偷偷改写后当作原话保存；必须在 conflicts 中明确告诉用户该原话不能直接发布，需要用户确认一个合规版本。
11. “按意思灵活生成”要把“必须保留的意思/事实”和“禁止扩写的内容”写清楚，避免运行时为了变化而新增事实。

只输出严格 JSON：
{
  "action":"PROPOSE_UPDATE|EXPLAIN",
  "target_layer":"L1|L2|L3",
  "assistant_message":"给用户看的自然语言回复",
  "updated_policy":"如果是 PROPOSE_UPDATE，这里给完整新策略；否则为空字符串",
  "change_summary":["变化1","变化2"],
  "conflicts":["如果与L1冲突，在这里写明"],
  "understood":["你从用户话里理解出的关键点"]
}
"""


def strategy_chat_message(data):
    config = load_strategy_chat_config()
    message = str(data.get("message") or "").strip()
    if not message:
        raise ValueError("message is empty")

    active_layer = str(data.get("active_layer") or config.get("active_layer") or "L3").upper()
    if active_layer not in ("L1", "L2", "L3"):
        active_layer = "L3"
    config["active_layer"] = active_layer

    # Natural confirmation of the last proposal.
    pending = config.get("pending_update")
    if pending and re.fullmatch(r"(对|对的|是|是的|确认|确认使用|应用|应用吧|就这样|可以|没问题)[。！! ]*", message):
        target = str(pending.get("target_layer") or active_layer)
        field = {"L1": "l1_policy", "L2": "l2_policy", "L3": "l3_policy"}.get(target)
        if field and pending.get("updated_policy"):
            config[field] = str(pending["updated_policy"]).strip()
            config["pending_update"] = None
            config = save_strategy_chat_config(config)
            reply = "已经应用到%s。现在右侧对应策略就是最新版本，你可以继续用自然语言告诉我哪里还不合适。" % target
            append_strategy_chat_history("user", message)
            append_strategy_chat_history("assistant", reply, {"action": "APPLIED", "target_layer": target})
            return {
                "reply": reply,
                "action": "APPLIED",
                "target_layer": target,
                "config": config,
                "effective_policy": build_effective_strategy(config),
            }

    if pending and re.fullmatch(r"(不要|不对|取消|先别改|不应用)[。！! ]*", message):
        config["pending_update"] = None
        config = save_strategy_chat_config(config)
        reply = "好，这次建议不应用。你直接告诉我哪里不对、希望怎么改，我重新理解。"
        append_strategy_chat_history("user", message)
        append_strategy_chat_history("assistant", reply, {"action": "CANCELLED"})
        return {
            "reply": reply,
            "action": "CANCELLED",
            "target_layer": active_layer,
            "config": config,
            "effective_policy": build_effective_strategy(config),
        }

    history = load_strategy_chat_history(12)
    history_text = "\n".join(
        ("%s：%s" % ("用户" if x.get("role") == "user" else "Agent", x.get("content", "")))
        for x in history[-8:]
    )
    payload = {
        "active_layer": active_layer,
        "industry_path": config.get("industry_path"),
        "l1_policy": config.get("l1_policy"),
        "l2_policy": config.get("l2_policy"),
        "l3_policy": config.get("l3_policy"),
        "current_effective_policy": build_effective_strategy(config),
        "recent_conversation": history_text,
        "user_message": message,
    }
    raw, model_ms, usage, cost = model_call(
        [
            {"role": "system", "content": STRATEGY_CONFIG_AGENT_SYSTEM},
            {"role": "user", "content": json.dumps(payload, ensure_ascii=False)},
        ],
        max_tokens=1200,
        temperature=0.32,
        top_p=0.75,
    )
    try:
        result = parse_json_object(raw)
    except Exception:
        result = {
            "action": "EXPLAIN",
            "target_layer": active_layer,
            "assistant_message": raw[:1200],
            "updated_policy": "",
            "change_summary": [],
            "conflicts": [],
            "understood": [],
        }

    action = str(result.get("action") or "EXPLAIN").upper()
    target_layer = str(result.get("target_layer") or active_layer).upper()
    if target_layer not in ("L1", "L2", "L3"):
        target_layer = active_layer
    reply = str(result.get("assistant_message") or "").strip()
    updated_policy = str(result.get("updated_policy") or "").strip()

    if action == "PROPOSE_UPDATE" and updated_policy:
        config["pending_update"] = {
            "target_layer": target_layer,
            "updated_policy": updated_policy,
            "change_summary": result.get("change_summary") or [],
            "conflicts": result.get("conflicts") or [],
            "understood": result.get("understood") or [],
            "created_at": time.strftime("%Y-%m-%dT%H:%M:%S"),
        }
        config = save_strategy_chat_config(config)
    else:
        action = "EXPLAIN"
        config["active_layer"] = active_layer
        config = save_strategy_chat_config(config)

    append_strategy_chat_history("user", message)
    append_strategy_chat_history(
        "assistant",
        reply,
        {
            "action": action,
            "target_layer": target_layer,
            "change_summary": result.get("change_summary") or [],
            "conflicts": result.get("conflicts") or [],
        },
    )
    return {
        "reply": reply,
        "action": action,
        "target_layer": target_layer,
        "proposal": config.get("pending_update"),
        "config": config,
        "effective_policy": build_effective_strategy(config),
        "model_ms": model_ms,
        "usage": usage,
        "cost_cny": round(cost, 8),
    }

def parse_facts(text):
    facts = {}
    for line in str(text or "").splitlines():
        line = line.strip()
        if not line or "=" not in line:
            continue
        k, v = line.split("=", 1)
        k, v = k.strip(), v.strip()
        if k and v:
            facts[k] = v
    return facts


def get_recent_speech(room_id, limit=3):
    key = "livecompanion:room:" + str(room_id) + ":recent_speech"
    try:
        values = redis_cmd("LRANGE", key, "0", str(max(int(limit) - 1, 0)))
        return values or []
    except Exception:
        return []


def remember_speech(room_id, text):
    text = str(text or "").strip()
    if not text:
        return
    key = "livecompanion:room:" + str(room_id) + ":recent_speech"
    try:
        redis_cmd("LPUSH", key, text)
        redis_cmd("LTRIM", key, "0", "5")
        redis_cmd("EXPIRE", key, "3600")
    except Exception:
        pass


def get_recent_interaction_variants(room_id, intent, limit=5):
    safe_intent = re.sub(r"[^a-zA-Z0-9_\-]+", "_", str(intent or "unknown"))[:80]
    key = "livecompanion:room:%s:interaction_variants:%s" % (room_id, safe_intent)
    try:
        values = redis_cmd("LRANGE", key, "0", str(max(int(limit) - 1, 0)))
        return values or []
    except Exception:
        return []


def remember_interaction_variant(room_id, intent, text):
    text = str(text or "").strip()
    if not text:
        return
    safe_intent = re.sub(r"[^a-zA-Z0-9_\-]+", "_", str(intent or "unknown"))[:80]
    key = "livecompanion:room:%s:interaction_variants:%s" % (room_id, safe_intent)
    try:
        redis_cmd("LPUSH", key, text)
        redis_cmd("LTRIM", key, "0", "7")
        redis_cmd("EXPIRE", key, "3600")
    except Exception:
        pass


def model_call(messages, max_tokens=220, temperature=0.58, top_p=0.82):
    payload = {
        "model": MODEL,
        "messages": messages,
        "enable_thinking": False,
        "stream": False,
        "max_tokens": max_tokens,
        "temperature": float(temperature),
        "top_p": float(top_p)
    }
    started = time.perf_counter()
    resp = requests.post(
        API_URL,
        headers={"Authorization": "Bearer " + get_api_key(), "Content-Type": "application/json"},
        json=payload,
        timeout=25
    )
    elapsed_ms = round((time.perf_counter() - started) * 1000, 1)
    if resp.status_code != 200:
        raise RuntimeError("模型接口 HTTP " + str(resp.status_code) + ": " + resp.text[:500])
    body = resp.json()
    usage = body.get("usage") or {}
    text = body["choices"][0]["message"]["content"].strip()
    prompt_tokens = int(usage.get("prompt_tokens") or 0)
    completion_tokens = int(usage.get("completion_tokens") or 0)
    cost = (prompt_tokens * INPUT_PRICE_PER_M + completion_tokens * OUTPUT_PRICE_PER_M) / 1000000
    return text, elapsed_ms, usage, cost

def write_trace(room_id, payload):
    key = "livecompanion:room:" + str(room_id) + ":ai_trace"
    fields = []
    for k, v in payload.items():
        if isinstance(v, (dict, list)):
            v = json.dumps(v, ensure_ascii=False, separators=(",", ":"))
        fields.extend([k, v])
    redis_cmd("XADD", key, "MAXLEN", "~", "10000", "*", *fields)
    redis_cmd("EXPIRE", key, str(TRACE_TTL_SECONDS))
    return key

def select_fact_keys(question, facts):
    q = (question or "").strip()
    if not q:
        return [], "unknown"

    if re.search(r"为什么|原因|怎么会", q):
        return [], "reason_unknown"

    if re.search(r"价格|多少钱|售价|价钱", q):
        return [], "price_dynamic"
    if re.search(r"库存|有货|没货|补贴|赠品|送什么|什么快递|哪家快递|今天.*发货|什么时候发货", q):
        return [], "dynamic_unknown"

    if re.search(r"原切", q):
        keys = [k for k in facts if k.startswith("original_cut_")]
        if re.search(r"蒸|清蒸|做法|怎么做", q):
            preferred = [k for k in keys if "steam" in k]
            return (preferred or keys[:2]), "original_cut"
        return keys[:2], "original_cut"

    if re.search(r"小母鸡|14号", q):
        keys = [k for k in facts if k.startswith("link_14_")]
        if re.search(r"多重|几斤|重量|净重|毛重", q):
            return [k for k in keys if "weight" in k], "link14_weight"
        if re.search(r"几个月|多大|月龄", q):
            return [k for k in keys if "age" in k], "link14_age"
        return keys[:2], "link14"

    if re.search(r"童子鸡|1号|一只鸡|一只|几斤|多重", q):
        if re.search(r"多重|几斤|重量|净重|毛重", q):
            return [k for k in facts if k.startswith("link_1_") and "weight" in k], "link1_weight"
        if re.search(r"生的|熟的|生鲜", q):
            return [k for k in facts if k == "link_1_state"], "link1_state"
        if re.search(r"公鸡|母鸡|公的|母的|几个月|多大|月龄", q):
            return [k for k in facts if k == "link_1_age_sex"], "link1_age_sex"

    if re.search(r"多久到|几天到|什么时候到|到货", q):
        return [k for k in facts if k.startswith("delivery_")], "delivery_eta"

    if re.search(r"怎么做|做法|怎么蒸|怎么煮|怎么炖|做汤|教程", q):
        return [k for k in facts if "cooking" in k], "cooking"

    return [], "unknown"

RISKY_UNGROUNDED_TERMS = [
    "包装", "冰袋", "去包", "带包", "损耗", "可食部分", "下锅的肉",
    "肉质", "嫩度", "柴", "纤维", "营养", "半成品", "开袋即食", "去完杂", "能吃的部分", "有多少肉", "实得肉", "纯肉",
    "保鲜方式", "冷链", "运输原因", "餐厅得卖", "外面得卖",
    "有机", "散养", "土鸡", "手慢无", "最后几单", "库存不多",
    "限时", "薅羊毛", "补贴", "赠品", "京东快递", "顺丰", "包邮"
]


def strip_ungrounded_risky_sentences(text, allowed_facts):
    allowed_text = " ".join(str(v) for v in (allowed_facts or {}).values())
    text = str(text or "").strip()
    # Remove common invented explanations while preserving the grounded number.
    text = text.replace("去完杂之后净重", "净重")
    text = text.replace("实际能吃的部分以这个净重区间为准", "净重这个口径就在这个区间")
    text = text.replace("到手到底有多少肉", "这只到底多重")
    parts = re.split(r"(?<=[。！？!?])", text)
    kept = []
    for part in parts:
        sentence = part.strip()
        if not sentence:
            continue
        bad = False
        for term in RISKY_UNGROUNDED_TERMS:
            if term in sentence and term not in allowed_text:
                bad = True
                break
        if not bad:
            kept.append(sentence)
    return "".join(kept).strip()


def clean_repetitive_resume_opening(text, kind):
    text = str(text or "").strip()
    if kind != "RESUME":
        return text

    # Resume should continue the topic, not narrate that it was interrupted.
    patterns = [
        r"^(?:行|好|好嘞|OK|哎)[，,、 ]*插曲结束[，,、 ]*(?:咱们)?(?:接着聊|继续聊|接着说|继续说)[。！!，, ]*",
        r"^(?:行|好|好嘞|OK|哎)[，,、 ]*刚才(?:那个|这个)?(?:小)?插曲.*?[。！!，,]",
        r"^(?:行|好|好嘞|OK|哎)[，,、 ]*刚才(?:那事儿|那茬儿|那事|那茬).*?(?:翻篇|放一放).*?[。！!，,]",
        r"^(?:行|好|好嘞|OK|哎)[，,、 ]*(?:咱们)?回到正题[。！!，, ]*",
        r"^(?:行|好|好嘞|OK|哎)[，,、 ]*(?:咱们)?(?:接着聊|继续聊|接着说|继续说)[。！!，, ]*",
    ]
    for pat in patterns:
        text = re.sub(pat, "", text, count=1)
    return text.strip()


def safe_fallback_text(allowed_facts):
    values = [str(v).strip().rstrip("。") for v in (allowed_facts or {}).values() if str(v).strip()]
    if not values:
        return "咱们先把这一段接住，具体信息以直播间已经确认的内容为准。"
    body = "；".join(values[:6])
    return "宝宝们，咱们先把这几个已经确认的信息捋清楚：" + body + "。这几个点先记住，咱们接着往下看。"


def fact_guard(draft, allowed_facts, phase, kind, recent_speech):
    payload = {
        "phase": phase,
        "kind": kind,
        "allowed_facts": allowed_facts,
        "recent_speech": recent_speech,
        "draft": draft
    }
    return model_call(
        [
            {"role": "system", "content": FACT_GUARD_SYSTEM},
            {"role": "user", "content": json.dumps(payload, ensure_ascii=False)}
        ],
        max_tokens=900 if phase == "PRODUCT_SHAPING" else 240,
        temperature=0.18,
        top_p=0.55
    )


def merge_usage(a, b):
    return {
        "prompt_tokens": int((a or {}).get("prompt_tokens") or 0) + int((b or {}).get("prompt_tokens") or 0),
        "completion_tokens": int((a or {}).get("completion_tokens") or 0) + int((b or {}).get("completion_tokens") or 0),
        "generation": a or {},
        "fact_guard": b or {}
    }


def generate_control(config, goal, fact_keys, kind, phase=None):
    facts = parse_facts(config.get("facts_text"))
    allowed = {k: facts[k] for k in fact_keys if k in facts}
    room_id = config.get("room_id", "unknown")
    recent = get_recent_speech(room_id, 3)

    if kind == "MAINLINE" and phase == "PRODUCT_SHAPING":
        style_target = "塑品必须一次讲完整，生成一整段320到520字、10到18句的真人直播口播。不要按参数顺序念，要做2到4个小波次：拉回注意力/抛观众问题→用allowed_facts连续塑品→自然提醒选择或链接→再换角度继续。必须覆盖1号童子鸡、原切、14号小母鸡并完整收束。可以有轻CTA，但没有动态价格/券/库存事实时绝不能编造紧迫感。禁止以“后面再讲/接下来再说”半截结束。"
        max_tokens = 900
    elif kind == "MAINLINE":
        style_target = "主线要连续、自然、有承接，避免模板腔和复读。"
        max_tokens = 260
    elif kind == "RESUME":
        style_target = "只做自然桥接，不重复刚才整段主线。"
        max_tokens = 100
    else:
        style_target = "插话短、准，回答完便于马上回主线。"
        max_tokens = 120

    data = {
        "kind": kind,
        "phase": phase,
        "control_goal": goal,
        "allowed_facts": allowed,
        "recent_speech": recent,
        "style_target": style_target,
        "persona": config.get("persona", ""),
        "customer_rules": config.get("answer_rules", ""),
        "speech_plan_template": config.get("speech_plan_template", "") if phase == "PRODUCT_SHAPING" else ""
    }

    draft, gen_ms, gen_usage, gen_cost = model_call(
        [
            {"role": "system", "content": CONTROL_SYSTEM},
            {"role": "user", "content": json.dumps(data, ensure_ascii=False)}
        ],
        max_tokens=max_tokens,
        temperature=0.58,
        top_p=0.82
    )

    final_text, guard_ms, guard_usage, guard_cost = fact_guard(
        draft, allowed, phase, kind, recent
    )
    final_text = strip_ungrounded_risky_sentences(final_text, allowed)
    final_text = clean_repetitive_resume_opening(final_text, kind)
    if kind == "MAINLINE" and len(final_text) < 55:
        final_text = safe_fallback_text(allowed)

    remember_speech(room_id, final_text)
    return (
        final_text,
        round(gen_ms + guard_ms, 1),
        merge_usage(gen_usage, guard_usage),
        gen_cost + guard_cost
    )


def split_semantic_chunks(text, min_chars=58, target_chars=88, max_chars=118):
    """Split one complete generated speech into interrupt-safe semantic blocks.

    We only cut at sentence boundaries. This preserves the original full speech
    while giving the director safe places to answer a live question and then
    resume from a bookmark.
    """
    text = str(text or "").strip()
    if not text:
        return []

    sentences = re.findall(r".+?(?:[。！？!?]+|$)", text)
    sentences = [s.strip() for s in sentences if s.strip()]
    if not sentences:
        return [text]

    chunks = []
    buf = ""
    for sentence in sentences:
        candidate = buf + sentence
        if buf and len(candidate) > max_chars and len(buf) >= min_chars:
            chunks.append(buf.strip())
            buf = sentence
        else:
            buf = candidate

        if len(buf) >= target_chars and re.search(r"[。！？!?]$", buf):
            chunks.append(buf.strip())
            buf = ""

    if buf:
        if chunks and len(buf) < 28:
            chunks[-1] = (chunks[-1] + buf).strip()
        else:
            chunks.append(buf.strip())

    # Keep chunks reasonably sized even for an unusually long single sentence.
    normalized = []
    for chunk in chunks:
        if len(chunk) <= max_chars + 30:
            normalized.append(chunk)
            continue
        parts = re.split(r"(?<=[，；：,;:])", chunk)
        temp = ""
        for part in parts:
            if not part:
                continue
            if temp and len(temp + part) > max_chars:
                normalized.append(temp.strip())
                temp = part
            else:
                temp += part
        if temp:
            normalized.append(temp.strip())

    return [c for c in normalized if c]



def parse_strict_json_text(text):
    text = str(text or "").strip()
    fence = chr(96) * 3
    if text.startswith(fence):
        text = text[len(fence):].lstrip()
        if text.lower().startswith("json"):
            text = text[4:].lstrip()
        if text.endswith(fence):
            text = text[:-len(fence)].rstrip()
    first = text.find("{")
    last = text.rfind("}")
    if first >= 0 and last > first:
        text = text[first:last + 1]
    return json.loads(text)


def normalize_full_plan_sections(obj):
    sections = obj.get("sections") if isinstance(obj, dict) else None
    if not isinstance(sections, list) or len(sections) != 7:
        raise ValueError("完整口播必须正好7段")

    titles = [
        "把人拉进当前商品",
        "主商品完整塑品",
        "把产品讲深",
        "原切/规格/变体说明",
        "第二商品/其他链接",
        "链接与选择动作",
        "完整收口",
    ]
    out = []
    for idx, item in enumerate(sections, start=1):
        if not isinstance(item, dict):
            raise ValueError("完整口播段落格式错误")
        text = str(item.get("text") or "").strip()
        if not text:
            raise ValueError("完整口播第%d段为空" % idx)
        out.append({
            "section": idx,
            "title": str(item.get("title") or titles[idx - 1]).strip() or titles[idx - 1],
            "text": text,
        })
    return out



FULL_PLAN_EXTRA_RISKY = [
    "去头去爪后的净重", "去头去爪后净重", "去掉内脏后的净重", "处理后的净重",
    "月龄小", "肉质更细嫩", "肉质细嫩", "更嫩", "老母鸡", "关键指标",
    "用途", "口感预期", "适合谁", "更适合", "嫌处理麻烦", "处理麻烦",
    "不后悔", "不吃亏", "直接决定", "烹饪预期", "肉量的需求",
    "不用自己费劲", "方便大家回家操作", "没缩水", "怕麻烦",
    "确保新鲜度", "新鲜度符合预期", "实物情况",
    "生长特性", "口感基础", "核心卖点", "真正能吃到嘴里的分量",
    "整只不好炖", "家里锅小", "分次吃", "特别适合",
    "特殊要求", "避免发错货", "按需求精准选择最重要",
    "环境好", "非常关键", "处理过程就很讲究", "处理过程很讲究",
    "嫌麻烦", "省去了处理", "仪式感", "子公鸡风味",
    "食品安全", "放养在", "意味着环境", "好山好水出好鸡",
    "吃不饱", "家里人多", "换个口味", "原汁原味", "直接冲",
    "很好的选择", "今天主推", "主推的还是", "腿部的肌肉", "肌肉线条"
]


def safe_fact_value(allowed, key):
    return str((allowed or {}).get(key) or "").strip()


def fallback_full_plan_section(section_num, allowed):
    l1 = safe_fact_value(allowed, "link_1_product")
    l1_state = safe_fact_value(allowed, "link_1_state")
    l1_age = safe_fact_value(allowed, "link_1_age_sex")
    l1_gross = safe_fact_value(allowed, "link_1_gross_weight")
    l1_net = safe_fact_value(allowed, "link_1_net_weight")
    oc_def = safe_fact_value(allowed, "original_cut_definition")
    oc_quality = safe_fact_value(allowed, "original_cut_quality")
    oc_steam = safe_fact_value(allowed, "original_cut_steam")
    cook = safe_fact_value(allowed, "cooking_help")
    l14 = safe_fact_value(allowed, "link_14_product")
    l14_age = safe_fact_value(allowed, "link_14_age")
    l14_gross = safe_fact_value(allowed, "link_14_gross_weight")
    l14_net = safe_fact_value(allowed, "link_14_net_weight")
    d1 = safe_fact_value(allowed, "delivery_jzh")
    d2 = safe_fact_value(allowed, "delivery_other")

    if section_num == 1:
        vals = [x for x in [l1, l14] if x]
        return "咱们先把今天直播间的核心选择说清楚。" + "；".join(vals) + "。先把这两个入口对上，选的时候就不容易搞混。"
    if section_num == 2:
        vals = [x for x in [l1_state, l1_age, l1_gross, l1_net] if x]
        return "先看1号童子鸡，这几个基础信息大家记准：" + "；".join(vals) + "。生熟、月龄公母和重量都对清楚，再决定是不是自己要的。"
    if section_num == 3:
        vals = [x for x in [l1_state, l1_gross, l1_net] if x]
        return "这个地方大家最容易搞混的是生熟和重量口径。" + "；".join(vals) + "。这几个都是已经确认的信息，按这几个点理解就行，不要把毛重和净重混成一个数。"
    if section_num == 4:
        vals = [x for x in [oc_def, oc_quality, oc_steam, cook] if x]
        return "再把原切说清楚：" + "；".join(vals) + "。这样整只和原切是什么关系，大家就能听明白了。"
    if section_num == 5:
        vals = [x for x in [l14, l14_age, l14_gross, l14_net] if x]
        return "再看14号：" + "；".join(vals) + "。这里就按14号已经确认的规格来理解，不和1号混在一起。"
    if section_num == 6:
        vals = [x for x in [l1, l14, d1, d2] if x]
        return "准备下单前，把链接和已知信息再核对一遍：" + "；".join(vals) + "。确认自己要的是哪一个，再去对应链接看。"
    vals = [x for x in [l1, l1_net, l14, l14_net] if x]
    return "最后把这一轮压缩一下：" + "；".join(vals) + "。看准链接、核对规格，有不清楚的再问，确认以后再下单。"


def sanitize_full_plan_section(text, allowed, section_num):
    text = str(text or "").strip()

    replacements = {
        "去头去爪后的净重": "净重",
        "去头去爪后净重": "净重",
        "去掉内脏后的净重": "净重",
        "处理后的净重": "净重",
        "14号是生的小母鸡": "14号是小母鸡",
        "14号是生鲜小母鸡": "14号是小母鸡",
        "14号小母鸡是生的": "14号是小母鸡",
        "品质和整只的一样，没缩水": "品质和整只的一样",
        "品质跟整只的一模一样，没有任何区别": "品质和整只的一样",
        "怕麻烦选原切，品质不变": "原切鸡品质和整只的一样",
        "根据你对肉量的需求来选": "看清各自规格再选",
    }
    for a, b in replacements.items():
        text = text.replace(a, b)

    # In the product-story sections, talk about the product itself.
    # Concentrate "link" language in section 6 so the anchor does not sound like a menu robot.
    if section_num <= 5:
        text = text.replace("1号链接的童子鸡", "1号童子鸡")
        text = text.replace("14号链接的小母鸡", "14号小母鸡")
        text = text.replace("1号链接", "1号")
        text = text.replace("14号链接", "14号")

    text = text.replace("今天咱们重点聊的是生鲜鸡肉", "今天咱们重点把1号童子鸡和14号小母鸡讲清楚")
    text = text.replace("为什么强调“没开叫”？虽然咱们没有具体的养殖工艺数据，但这个月龄本身就是核心。", "“没开叫”这个信息大家也记一下，事实库里确认的是4到5个月、还没开叫的子公鸡。")
    text = text.replace("为什么强调“没开叫”？", "“没开叫”这个信息大家也记一下。")
    text = text.replace("因为活动空间大，所以这些鸡平时的运动量特别大，这一点非常关键。", "这些鸡平时活动量比较大。")
    text = text.replace("因为活动空间大，所以这些鸡平时的运动量特别大。", "这些鸡平时活动量比较大。")
    text = text.replace("很多人会问，运动量大对鸡肉有什么影响？", "你们收到货以后，再看鸡本身的状态。")
    text = text.replace("全部是放养在漫山遍野里的跑山鸡、溜达鸡", "不圈养，是漫山遍野活动的跑山鸡、溜达鸡")
    text = text.replace("因为是在高山上活动，这鸡平时的运动量非常大。", "这鸡平时活动量比较大。")
    text = text.replace("当然光靠这些肯定吃不饱嘛，所以我们还会喂", "同时，我们还会喂")
    text = text.replace("处理过程也很讲究，你看这只鸡，", "你看这只鸡，")
    text = text.replace("处理过程很讲究，你看这只鸡，", "你看这只鸡，")
    text = text.replace("，一点都没少", "")
    text = text.replace("，非常方便", "")
    text = text.replace("1号这款童子鸡", "咱们这款童子鸡")
    text = text.replace("1号这款", "咱们这款")
    text = text.replace("有人担心切块影响品质，完全不会，原切鸡的品质和整只的是一模一样的", "原切鸡的品质和整只的一样")
    text = text.replace("这个细节骗不了人", "这个细节大家可以看一下")
    text = text.replace("这点大家放心", "这个信息大家可以记一下")
    text = text.replace("全部都是活鸡新鲜宰杀", "目前下单的是活鸡新鲜宰杀")
    text = text.replace("全部是活鸡新鲜宰杀", "目前下单的是活鸡新鲜宰杀")
    text = re.sub(r"很多人后台私信问我[，,]?说?", "很多人会关心", text)
    text = re.sub(r"后台(?:很多人)?私信(?:问我)?[，,]?", "很多人会关心，", text)
    text = text.replace("所以你们收到货以后", "你们收到货以后")
    text = text.replace("正宗的安徽皖南", "安徽皖南")
    text = text.replace("正儿八经的纯种", "纯种")
    text = re.sub(r"^(?:来[，,]?)?咱们接着聊(?:一下)?这只鸡[。！!，,]*", "", text).strip()

    text = strip_ungrounded_risky_sentences(text, allowed)
    allowed_text = " ".join(str(v) for v in (allowed or {}).values())

    parts = re.split(r"(?<=[。！？!?])", text)
    kept = []
    for part in parts:
        sentence = part.strip()
        if not sentence:
            continue

        bad = False
        for term in FULL_PLAN_EXTRA_RISKY:
            if term in sentence and term not in allowed_text:
                bad = True
                break

        if not safe_fact_value(allowed, "link_14_state"):
            if "14号" in sentence and ("生鲜" in sentence or re.search(r"14号.{0,10}是生的", sentence)):
                bad = True

        if "包装" in sentence and "教程入口" not in sentence and "做法" not in sentence:
            bad = True

        if not bad:
            kept.append(sentence)

    cleaned = "".join(kept).strip()
    if len(cleaned) < 35:
        cleaned = fallback_full_plan_section(section_num, allowed)
    return cleaned



def split_continuous_speech_exact(text, chunk_count=7):
    text = str(text or "").strip()
    if not text:
        return []

    sentences = [x.strip() for x in re.split(r"(?<=[。！？!?])", text) if x.strip()]
    if len(sentences) < chunk_count:
        # Very rare fallback: use semantic chunks first, then split the longest chunks
        # at natural comma/semicolon boundaries until there are enough playback blocks.
        chunks = split_semantic_chunks(text, min_chars=45, target_chars=90, max_chars=150)
        while len(chunks) < chunk_count:
            idx = max(range(len(chunks)), key=lambda i: len(chunks[i]))
            parts = [x.strip() for x in re.split(r"(?<=[，；、,;])", chunks[idx]) if x.strip()]
            if len(parts) < 2:
                break
            mid = max(1, len(parts) // 2)
            chunks[idx:idx+1] = ["".join(parts[:mid]), "".join(parts[mid:])]
        return chunks[:chunk_count]

    total_len = sum(len(x) for x in sentences)
    target = max(total_len / chunk_count, 1)
    chunks = []
    current = ""
    consumed = 0

    for i, sentence in enumerate(sentences):
        remaining_sentences = len(sentences) - i
        remaining_chunks = chunk_count - len(chunks)

        if current and len(chunks) < chunk_count - 1:
            projected = len(current) + len(sentence)
            must_leave = remaining_sentences >= remaining_chunks
            if projected > target * 1.18 and must_leave:
                chunks.append(current)
                consumed += len(current)
                current = ""
                remaining_chunks = chunk_count - len(chunks)
                remaining_len = max(total_len - consumed, 1)
                target = remaining_len / max(remaining_chunks, 1)

        current += sentence

        # Ensure exactly enough sentence groups remain for future chunks.
        sentences_left = len(sentences) - i - 1
        chunks_left = chunk_count - len(chunks) - 1
        if current and len(chunks) < chunk_count - 1 and sentences_left == chunks_left:
            chunks.append(current)
            consumed += len(current)
            current = ""
            remaining_chunks = chunk_count - len(chunks)
            remaining_len = max(total_len - consumed, 1)
            target = remaining_len / max(remaining_chunks, 1)

    if current:
        chunks.append(current)

    # Merge overflow into the final chunk; pad rare underflow by natural split.
    if len(chunks) > chunk_count:
        chunks = chunks[:chunk_count-1] + ["".join(chunks[chunk_count-1:])]

    while len(chunks) < chunk_count:
        idx = max(range(len(chunks)), key=lambda i: len(chunks[i]))
        part = chunks[idx]
        split_at = max(part.rfind("，", 0, len(part)//2 + 20), part.rfind("；", 0, len(part)//2 + 20))
        if split_at <= 10 or split_at >= len(part)-10:
            break
        chunks[idx:idx+1] = [part[:split_at+1].strip(), part[split_at+1:].strip()]

    return [x for x in chunks if x]



VARIATION_OPENINGS = [
    {
        "id": "product_direct",
        "instruction": "直接从当前主商品身份切入，1~2句进入产品，不喊口号，不用“大家把目光聚焦”。"
    },
    {
        "id": "question_hook",
        "instruction": "用一个观众常见、且答案能被 allowed_facts 直接支持的问题起头，马上回答并进入商品主线。不要虚构“刚有人问、后台私信、评论区有人说”等真实互动事件。"
    },
    {
        "id": "detail_first",
        "instruction": "先挑一个 allowed_facts 里可观察或可确认的具体细节起头，再自然回到商品身份和来源。"
    },
    {
        "id": "origin_first",
        "instruction": "如果有来源/产地/养殖事实，从“这只商品怎么来的”切入；没有就退回商品身份切入。"
    },
    {
        "id": "consumer_confusion",
        "instruction": "从消费者最容易搞混的一个真实点切入，例如生熟、整只/原切、毛重/净重；解释一句后马上回到完整塑品。"
    },
]

VARIATION_CADENCES = [
    {
        "id": "long_flow",
        "instruction": "以2~4句连续长口语为一个小话题块，块与块之间用自然承接，不做报告式分段。"
    },
    {
        "id": "question_answer_flow",
        "instruction": "中间穿插1~2个自然反问/自问自答，但答案必须来自已知事实；不要连续反问。"
    },
    {
        "id": "observation_flow",
        "instruction": "多用“你看、你们收到后可以看、这个地方注意一下”这类观察式承接，但每种口头语最多用一次。"
    },
    {
        "id": "plain_story_flow",
        "instruction": "少用提问，多像真人顺着商品故事往下说；句子长短交替，避免每句都同一个节奏。"
    },
]

VARIATION_CTA_STYLES = [
    {
        "id": "soft_choice",
        "instruction": "收口只做一次轻选择动作：确认规格/对应链接，不突然变成客服或促销口号。"
    },
    {
        "id": "fact_recap",
        "instruction": "收口先自然压回1~2个核心事实，再顺手带一句选择动作，不用“总结一下”。"
    },
    {
        "id": "question_cta",
        "instruction": "收口允许用一句问题引导评论或确认规格，再带购买动作；不能制造紧迫感。"
    },
]

VARIATION_FOCUS_GROUPS = [
    ("identity_source", ["link_1_product", "breed", "origin"]),
    ("raising_activity", ["raising_method", "raising_environment", "movement"]),
    ("feeding_detail", ["feeding"]),
    ("appearance_meat", ["skin_meat"]),
    ("slaughter_offal", ["slaughter", "offal"]),
    ("spec_state", ["link_1_state", "link_1_age_sex", "link_1_gross_weight", "link_1_net_weight"]),
    ("original_cut", ["original_cut_definition", "original_cut_quality", "original_cut_steam"]),
    ("delivery_cooking", ["delivery_jzh", "delivery_other", "cooking_help", "packaging", "courier", "cooking_recommendation"]),
]


def get_recent_full_rounds(room_id, limit=8):
    key = "livecompanion:room:" + str(room_id) + ":recent_full_rounds"
    try:
        values = redis_cmd("LRANGE", key, "0", str(max(int(limit) - 1, 0)))
        return values or []
    except Exception:
        return []


def remember_full_round(room_id, text):
    text = str(text or "").strip()
    if not text:
        return
    key = "livecompanion:room:" + str(room_id) + ":recent_full_rounds"
    try:
        redis_cmd("LPUSH", key, text)
        redis_cmd("LTRIM", key, "0", "9")
        redis_cmd("EXPIRE", key, "21600")
    except Exception:
        pass


def next_variation_profile(room_id, allowed):
    counter_key = "livecompanion:room:" + str(room_id) + ":speech_variation_round"
    try:
        round_no = int(redis_cmd("INCR", counter_key) or 1)
        redis_cmd("EXPIRE", counter_key, "604800")
    except Exception:
        round_no = int(time.time()) % 100000

    available_groups = []
    for group_id, keys in VARIATION_FOCUS_GROUPS:
        present = [k for k in keys if str(allowed.get(k) or "").strip()]
        if present:
            available_groups.append({"id": group_id, "fact_keys": present})

    if available_groups:
        start = (round_no - 1) % len(available_groups)
        rotated = available_groups[start:] + available_groups[:start]
        focus = rotated[:min(3, len(rotated))]
    else:
        focus = []

    opening = VARIATION_OPENINGS[(round_no - 1) % len(VARIATION_OPENINGS)]
    cadence = VARIATION_CADENCES[(round_no - 1) % len(VARIATION_CADENCES)]
    cta = VARIATION_CTA_STYLES[(round_no - 1) % len(VARIATION_CTA_STYLES)]

    return {
        "round_no": round_no,
        "mode": "STABLE_SKELETON_DYNAMIC_EXPRESSION",
        "opening": opening,
        "cadence": cadence,
        "cta": cta,
        "focus_groups": focus,
        "rule": "整体故事骨架保持稳定；本轮重点展开 focus_groups，其余真实信息简洁带过。表达、承接、开头和收尾要避开最近几轮原句。"
    }


def compact_recent_rounds(rounds, limit=6, max_chars=680):
    items = []
    for i, text in enumerate((rounds or [])[:limit]):
        t = str(text or "").strip()
        if not t:
            continue
        if len(t) > max_chars:
            t = t[:max_chars]
        items.append({"age": i + 1, "text": t})
    return items


def _surface_normalize(text):
    text = re.sub(r"\s+", "", str(text or ""))
    text = re.sub(r"[，。！？、；：,.!?;:“”‘’（）()\-—…]", "", text)
    return text


def speech_surface_similarity(a, b):
    from difflib import SequenceMatcher
    aa = _surface_normalize(a)
    bb = _surface_normalize(b)
    if not aa or not bb:
        return 0.0
    return round(SequenceMatcher(None, aa, bb, autojunk=False).ratio(), 4)


def longest_surface_overlap(a, b):
    from difflib import SequenceMatcher
    aa = _surface_normalize(a)
    bb = _surface_normalize(b)
    if not aa or not bb:
        return 0
    match = SequenceMatcher(None, aa, bb, autojunk=False).find_longest_match()
    return int(match.size)


def max_recent_similarity(text, recent_rounds):
    scores = [speech_surface_similarity(text, old) for old in (recent_rounds or []) if str(old or "").strip()]
    return max(scores) if scores else 0.0


FULL_SPEECH_VARIATION_REWRITE_SYSTEM = """你是直播口播“换一种说法”编辑器。

输入包含：
- allowed_facts：唯一事实边界；
- draft_text：当前草稿；
- recent_rounds：最近几轮已经播过的话术；
- variation_profile：本轮变化策略。

只输出严格 JSON：
{"text":"改写后的完整连续口播"}

目标不是改变商品框架，而是让同一套真实商品信息每轮听起来都不一样。

必须做到：
- 保留 draft_text 的整体商品主线和所有被 allowed_facts 支持的重要事实。
- 不增加新的可核查商品事实。
- 换开场方式、换句式、换承接、换解释角度、换信息轻重、换收尾表达。
- 核心事实可以重复，但尽量不要连续复用 recent_rounds 中相同的10个以上连续汉字。
- 不要把话术改成参数表、标题、编号、问答列表。
- 不要为了“创新”编新场景、新功效、新对比、新价格、新库存、新优惠。
- 不要虚构观众行为或实时互动，例如“后台很多人私信”“刚有人问”“评论区都在问”“老饕都特别在意”，除非输入真的提供了该事件。
- 避免“这个细节骗不了人”“这点大家放心”“百分百”“全部都”“肯定”等带绝对或保证意味的说法；有对应事实时改成中性、可核对的表达。
- 遵守非绝对、非保证、非最高级的广告合规表达。
- variation_profile 只决定本轮怎么变化；稳定商品骨架不能丢。
"""

def generate_full_speech_plan(config):
    facts = parse_facts(config.get("facts_text"))
    facts.update(parse_facts(config.get("deep_facts_text")))
    allowed = {
        k: v for k, v in facts.items()
        if str(v).strip() and not str(k).startswith("link_14_")
    }
    template = str(config.get("speech_plan_template") or "").strip()
    room_id = config.get("room_id", "unknown")
    recent_rounds = get_recent_full_rounds(room_id, 8)
    recent_rounds_compact = compact_recent_rounds(recent_rounds, 6, 680)
    variation_profile = next_variation_profile(room_id, allowed)

    payload = {
        "speech_plan_template": template,
        "reference_script": str(config.get("reference_script") or ""),
        "allowed_facts": allowed,
        "recent_rounds": recent_rounds_compact,
        "variation_profile": variation_profile,
        "persona": config.get("persona", ""),
        "customer_rules": config.get("answer_rules", ""),
        "instruction": (
            "采用“稳定骨架+动态表达”。整体商品故事框架大致保持一致；"
            "本轮按 variation_profile 改变开头、重点细节、解释角度、承接、句式和收口。"
            "不要复刻 recent_rounds 的长句。主商品一路讲到底，本轮不要主动切换第二商品；"
            "链接最多在收尾自然带一句。"
        )
    }

    draft_text, gen_ms, gen_usage, gen_cost = model_call(
        [
            {"role": "system", "content": FULL_SPEECH_PLAN_SYSTEM},
            {"role": "user", "content": json.dumps(payload, ensure_ascii=False)}
        ],
        max_tokens=2200,
        temperature=0.72,
        top_p=0.90
    )

    obj = parse_strict_json_text(draft_text)
    raw_text = str(obj.get("text") or "").strip() if isinstance(obj, dict) else ""
    if not raw_text:
        raise ValueError("连续口播模型没有返回 text")

    draft_similarity = max_recent_similarity(raw_text, recent_rounds)
    # Keep live latency stable: variation is driven in the first generation call.
    # Similarity is observed and traced, but we do not add a third rewrite-model call.
    rewrite_ms = 0.0
    rewrite_usage = {}
    rewrite_cost = 0.0

    guard_payload = {
        "allowed_facts": allowed,
        "draft_text": raw_text
    }
    guarded_text, guard_ms, guard_usage, guard_cost = model_call(
        [
            {"role": "system", "content": FULL_SPEECH_CONTINUOUS_GUARD_SYSTEM},
            {"role": "user", "content": json.dumps(guard_payload, ensure_ascii=False)}
        ],
        max_tokens=1800,
        temperature=0.12,
        top_p=0.45
    )
    guarded_obj = parse_strict_json_text(guarded_text)
    guarded_full = str(guarded_obj.get("text") or "").strip() if isinstance(guarded_obj, dict) else ""
    if not guarded_full:
        guarded_full = raw_text

    # Final deterministic cleanup after the semantic fact guard.
    full_text = sanitize_full_plan_section(guarded_full, allowed, 3)

    deep_keys = {
        "origin", "breed", "raising_method", "raising_environment", "movement",
        "feeding", "skin_meat", "slaughter", "offal", "packaging",
        "courier", "cooking_recommendation"
    }
    has_deep_product_material = any(str(allowed.get(k) or "").strip() for k in deep_keys)
    if not has_deep_product_material and len(full_text) < 120:
        full_text = fallback_full_plan_section(3, allowed)

    final_similarity = max_recent_similarity(full_text, recent_rounds)

    chunks = split_continuous_speech_exact(full_text, 7)
    if len(chunks) != 7:
        chunks = split_semantic_chunks(
            full_text,
            min_chars=35,
            target_chars=max(55, len(full_text)//7),
            max_chars=max(90, len(full_text)//5)
        )
        if len(chunks) > 7:
            chunks = chunks[:6] + ["".join(chunks[6:])]

    sections = [
        {"section": i + 1, "title": "连续口播播放块 %d/7" % (i + 1), "text": chunk}
        for i, chunk in enumerate(chunks)
    ]

    for chunk in chunks:
        remember_speech(room_id, chunk)
    remember_full_round(room_id, full_text)

    total_ms = round(gen_ms + rewrite_ms + guard_ms, 1)
    trace_id = uuid.uuid4().hex[:16]
    total_cost = gen_cost + rewrite_cost + guard_cost
    merged_usage = merge_usage(gen_usage, rewrite_usage)
    merged_usage = merge_usage(merged_usage, guard_usage)

    write_trace(room_id, {
        "stage": "full_speech_plan",
        "trace_id": trace_id,
        "monitor_action": "STABLE_SKELETON_DYNAMIC_EXPRESSION",
        "phase": "PRODUCT_SHAPING",
        "final_text": full_text,
        "chunk_count": len(chunks),
        "plan_mode": "STABLE_SKELETON_DYNAMIC_EXPRESSION",
        "variation_round": variation_profile.get("round_no"),
        "variation_opening": variation_profile.get("opening", {}).get("id"),
        "variation_cadence": variation_profile.get("cadence", {}).get("id"),
        "variation_focus": ",".join(x.get("id", "") for x in variation_profile.get("focus_groups", [])),
        "draft_recent_similarity": draft_similarity,
        "final_recent_similarity": final_similarity,
        "recent_round_count": len(recent_rounds),
        "control_ms": total_ms,
        "total_ms": total_ms,
        "cost_cny": round(total_cost, 8),
        "created_at": time.strftime("%Y-%m-%dT%H:%M:%S")
    })

    return {
        "trace_id": trace_id,
        "plan_mode": "STABLE_SKELETON_DYNAMIC_EXPRESSION",
        "section_count": len(chunks),
        "sections": sections,
        "speech_chunks": chunks,
        "full_text": full_text,
        "control_text": full_text,
        "variation_profile": variation_profile,
        "recent_round_count": len(recent_rounds),
        "draft_recent_similarity": draft_similarity,
        "final_recent_similarity": final_similarity,
        "control_ms": total_ms,
        "total_ms": total_ms,
        "usage": merged_usage,
        "cost_cny": round(total_cost, 8)
    }

def mainline(config, phase, segment_index=0, resume=False):
    phase = phase if phase in STAGES else "OPENING"
    spec = STAGES[phase]
    segments = spec["segments"]
    segment_index = int(segment_index or 0) % max(len(segments), 1)
    segment = segments[segment_index]
    facts = parse_facts(config.get("facts_text"))
    fact_keys = [k for k in segment["facts"] if k in facts]
    trace_id = uuid.uuid4().hex[:16]
    started = time.perf_counter()

    if resume:
        goal = "刚刚处理完插入事件，现在用一句自然桥接语回到“" + spec["label"] + "”主线，接着继续这个内容：" + segment["goal"]
        kind = "RESUME"
    else:
        goal = segment["goal"]
        kind = "MAINLINE"

    monitor = {
        "action": "RESUME_MAINLINE" if resume else "SPEAK_MAINLINE",
        "phase": phase,
        "phase_label": spec["label"],
        "segment_index": segment_index,
        "segment_count": len(segments),
        "control_goal": goal,
        "fact_keys": fact_keys,
        "priority": 5,
        "interruptible": spec["interruptible"],
        "next_phase": spec["next"]
    }

    text, control_ms, usage, cost = generate_control(config, goal, fact_keys, kind, phase=phase)
    total_ms = round((time.perf_counter() - started) * 1000, 1)

    speech_chunks = []
    plan_mode = "SINGLE_UTTERANCE"
    if phase == "PRODUCT_SHAPING" and not resume:
        speech_chunks = split_semantic_chunks(text)
        if len(speech_chunks) > 1:
            plan_mode = "BOOKMARKED_SEMANTIC_CHUNKS"

    write_trace(config["room_id"], {
        "stage": "resume" if resume else "mainline",
        "trace_id": trace_id,
        "monitor_action": monitor["action"],
        "phase": phase,
        "segment_index": segment_index,
        "control_goal": goal,
        "fact_keys": fact_keys,
        "final_text": text,
        "control_ms": control_ms,
        "total_ms": total_ms,
        "cost_cny": round(cost, 8),
        "plan_mode": plan_mode,
        "chunk_count": len(speech_chunks) if speech_chunks else 1,
        "created_at": time.strftime("%Y-%m-%dT%H:%M:%S")
    })

    return {
        "trace_id": trace_id,
        "monitor": monitor,
        "control_text": text,
        "control_ms": control_ms,
        "total_ms": total_ms,
        "usage": usage,
        "cost_cny": round(cost, 8),
        "plan_mode": plan_mode,
        "speech_chunks": speech_chunks,
        "chunk_count": len(speech_chunks) if speech_chunks else 1
    }

def should_suppress_live(intent):
    now = time.time()
    with COOLDOWN_LOCK:
        last = LAST_LIVE_INTENT.get(intent, 0.0)
        if now - last < 20:
            return True
        LAST_LIVE_INTENT[intent] = now
    return False

TIMELINE_MONITOR_SYSTEM = """你是直播间 Monitor Agent，只负责调度，不写主播长话术。
输入会给你：直播热度、观众问题、当前主线位置、系统已确认能否回答、可用事实键。
只输出严格 JSON，不要 markdown：
{
  "decision":"PREPARE_INTERRUPT|DEFER",
  "priority":"P1|P2|P3",
  "reason":"不超过30个中文字符"
}
规则：
1. 当前是“回答能力压力测试”，回答优先。只要是正常观众问题，原则上都 PREPARE_INTERRUPT。
2. facts_available=false 也不要因为缺少事实直接 DEFER；仍然安排回答，让主播明确说明“当前没有同步到该实时信息/现有资料不足”，而不是沉默。
3. P1：直接影响购买判断的明确商品问题，例如价格、库存、生熟、规格重量、物流时效、怎么做。
4. P2：商品理解问题，例如养殖、饲喂、肉质、产地、原切、商品特点。
5. P3：普通寒暄、低价值互动、与商品弱相关的问题。
6. COLD/WARM/HOT 测试阶段尽量回答；只有 VERY_HOT 且明显是 P3 低价值互动时才允许 DEFER。
7. 你的职责只是决定“要不要准备插话”。缺少直接事实不等于不能回答：可以安排主播用一般常识、合理推理和现有资料给出有帮助的信息，但不能把推理包装成已经确认的商品事实。"""

TIMELINE_INTERACTION_SYSTEM = """你是直播主播的实时交互话术生成器。
系统已经确定要回答，并给你当前主线、后续主线、已确认商品事实和可用于辅助理解的商品资料。
你要生成一段很短、自然、像真人主播顺手回答的插话，并在结尾自然留出回到主线的接口。

只输出严格 JSON，不要 markdown：
{
  "reply_core":"直接回答观众问题，1到3句",
  "resume_tail":"0到1句自然承接，禁止说回到正题/继续刚才/插曲结束",
  "covered_topics":["feeding"],
  "suggested_skip_sentence_ids":[]
}

硬规则：
1. 如果 answer_mode=VERIFIED_FACTS，reply_core 只能使用 allowed_facts，绝不能补充未提供事实；不得把“紧实”改成“香/好吃/鲜嫩”，也不得新增健康、营养、性价比、功效等评价。
2. 如果 answer_mode=REASONED_ASSIST，优先给用户真正有用的信息。先使用 context_facts，再用一般常识做合理解释、做法建议或可能性判断；使用“通常、一般来说、从目前资料看、可以理解为、可以这样做”这类非绝对表达。不要把两个商品事实用“因为/所以”硬连成已经确认的因果；如需解释原因，要明确这是一般经验性解释。不要拿别的商品作比较，不要新增未确认的“嫩、香、营养高、胶质多、脂肪低、功效好、不柴”等具体评价。遇到做法问题可以给清蒸、炖、煮等一般烹饪建议，但不要承诺做出来一定是什么口感。
3. 如果 answer_mode=DYNAMIC_CAUTION，价格、库存、券、赠品、快递品牌、当日发货、实时销量等动态信息不能猜具体数字或具体品牌；但不要只说“不知道”。说明这是实时信息，再告诉观众以商品页当前实际展示、可下单状态、结算页或客服确认为准；不要假设页面一定会显示具体库存数量。
4. 如果 answer_mode=GENERAL_CHAT，可以做简短自然的普通互动，也可以回答与商品相关的一般常识，但不要凭空新增当前商品的硬事实。涉及老人、小孩、孕妇、过敏、疾病等人群时，不要说“都能吃/都适合”，改成“要看个人情况”，再给通用、低风险的处理建议。
5. resume_tail 只能使用 resume_facts；它只负责把语气带向下一主线主题，不要复述 resume_facts 里的具体结论。优先使用“再看它平时活动这一块”“再说到收到货能看到的细节”这种指向式桥接，不要写“因为/所以/这跟……有关”这类因果桥接。
6. reply_core 必须先直接回答问题，不要用“这个我不知道”“这个不清楚”作为开头。
7. resume_tail 不要总结系统动作，不要说“刚才说到”“回到正题”“咱们继续”。
8. 如果回答内容已经完整覆盖后续某个 sentence，可把 sentence id 放进 suggested_skip_sentence_ids；不确定就留空。
9. covered_topics 只能从 allowed_topics 中选择。
10. 禁止“百分百、绝对、肯定、保证、一定、最好、最香、最嫩、完全没问题”这类绝对化或保证式词语。
11. 全部口播尽量控制在 90 字以内。"""


def load_policy_text(filename, fallback):
    section_map = {
        "monitor_policy.md": ("MONITOR_POLICY_START", "MONITOR_POLICY_END"),
        "interaction_policy.md": ("INTERACTION_POLICY_START", "INTERACTION_POLICY_END"),
    }
    if filename in section_map:
        master = POLICY_DIR / "strategy_policy.md"
        try:
            raw = master.read_text(encoding="utf-8")
            start_name, end_name = section_map[filename]
            pattern = (
                r"<!--\s*" + re.escape(start_name) + r"\s*-->"
                r"([\s\S]*?)"
                r"<!--\s*" + re.escape(end_name) + r"\s*-->"
            )
            match = re.search(pattern, raw)
            if match and match.group(1).strip():
                return match.group(1).strip()
        except Exception:
            pass
    path = POLICY_DIR / filename
    try:
        text = path.read_text(encoding="utf-8").strip()
        return text or fallback
    except Exception:
        return fallback


def load_policy_config():
    path = POLICY_DIR / "policy_config.json"
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
        if isinstance(data, dict):
            return data
    except Exception:
        pass
    return {
        "version": "fallback",
        "monitor": {
            "default_decision": "PREPARE_INTERRUPT",
            "very_hot_defer_p3": True,
            "priority": {
                "P1": {"min_safe_score": 65, "max_wait_ms": 9000},
                "P2": {"min_safe_score": 80, "max_wait_ms": 11000},
                "P3": {"min_safe_score": 80, "max_wait_ms": 14000},
            },
        },
        "interaction": {
            "dynamic_intents": ["price_dynamic", "dynamic_unknown"],
        },
    }


TIMELINE_MONITOR_SYSTEM = load_policy_text(
    "monitor_policy.md",
    TIMELINE_MONITOR_SYSTEM,
)
TIMELINE_INTERACTION_SYSTEM = load_policy_text(
    "interaction_policy.md",
    TIMELINE_INTERACTION_SYSTEM,
)
TIMELINE_POLICY_CONFIG = load_policy_config()


def timeline_combined_facts(config):
    facts = parse_facts(config.get("facts_text"))
    facts.update(parse_facts(config.get("deep_facts_text")))
    return facts


def select_timeline_fact_keys(question, facts):
    keys, intent = select_fact_keys(question, facts)
    if keys:
        return keys, intent

    q = str(question or "").strip()
    if re.search(r"包邮|运费|邮费|快递|当天发|今天发|几号发|什么时候发", q):
        return [], "dynamic_unknown"
    rules = [
        (r"吃什么|喂什么|饲料|喂养|饲喂|小虫|竹笋", ["feeding"], "feeding"),
        (r"哪里|哪儿|产地|哪的|什么地方|哪个地方|哪地方|哪里的", ["origin", "breed"], "origin"),
        (r"怎么养|养殖|圈养|跑山|溜达鸡|散养|活动量|运动量", ["raising_method", "raising_environment", "movement"], "raising"),
        (r"鸡皮|肉质|肥不肥|肥肉|瘦肉|紧实", ["skin_meat"], "skin_meat"),
        (r"现杀|现宰|活鸡|宰杀|新鲜宰", ["slaughter"], "slaughter"),
        (r"鸡胗|鸡肝|鸡心|内脏", ["offal"], "offal"),
    ]
    for pattern, wanted, label in rules:
        if re.search(pattern, q):
            return [k for k in wanted if k in facts], label

    if re.search(r"怎么样|什么特点|介绍一下|讲讲|值不值得看|这鸡好吗|这个鸡好吗", q):
        wanted = [
            "link_1_product", "origin", "breed", "raising_method", "movement",
            "feeding", "skin_meat", "slaughter", "offal",
            "link_1_age_sex", "link_1_gross_weight", "link_1_net_weight"
        ]
        return [k for k in wanted if k in facts], "product_overview"
    return [], intent


def fact_keys_to_topics(keys):
    out = []
    for k in keys or []:
        if k in ("origin", "breed"):
            out.append("identity_source")
        elif k in ("raising_method", "raising_environment", "movement"):
            out.append("raising_activity")
        elif k == "feeding":
            out.append("feeding")
        elif k == "skin_meat":
            out.append("appearance_meat")
        elif k in ("slaughter", "offal"):
            out.append("slaughter_offal")
        elif k.startswith("original_cut_"):
            out.append("original_cut")
        elif k.startswith("delivery_"):
            out.append("delivery")
        elif "cooking" in k:
            out.append("cooking")
        elif k.startswith("link_1_") or k.startswith("link_14_"):
            out.append("spec_state")
    return list(dict.fromkeys(out))


def facts_for_topics(facts, topics):
    mapping = {
        "identity_source": ["origin", "breed", "link_1_product"],
        "raising_activity": ["raising_method", "raising_environment", "movement"],
        "feeding": ["feeding"],
        "appearance_meat": ["skin_meat"],
        "slaughter_offal": ["slaughter", "offal"],
        "original_cut": ["original_cut_definition", "original_cut_quality", "original_cut_steam"],
        "spec_state": ["link_1_state", "link_1_age_sex", "link_1_gross_weight", "link_1_net_weight"],
        "delivery": ["delivery_jzh", "delivery_other"],
        "cooking": ["cooking_help", "cooking_recommendation"],
    }
    keys = []
    for topic in topics or []:
        keys.extend(mapping.get(topic, []))
    return {k: facts[k] for k in dict.fromkeys(keys) if k in facts}


def soften_absolute_language(text):
    text = str(text or "").strip()
    replacements = [
        ("百分百", "尽量"),
        ("绝对不会", "一般不会"),
        ("绝对", "相对"),
        ("肯定会", "通常会"),
        ("肯定", "一般来说"),
        ("保证", "尽量做到"),
        ("一定会", "通常会"),
        ("一定", "通常"),
        ("完全没问题", "一般是可以的"),
        ("最好", "比较合适"),
        ("最香", "香味会更明显一些"),
        ("最嫩", "口感会更嫩一些"),
        ("最正宗", "比较有这种风格"),
        ("完全支持", "可以"),
        ("完全可以", "可以"),
        ("那里显示的最准确", "以当时页面显示为准"),
        ("页面显示的最准确", "以当时页面显示为准"),
        ("最准确", "更直接"),
        ("，处理起来很方便", ""),
        ("处理起来很方便", ""),
    ]
    for old, new in replacements:
        text = text.replace(old, new)
    return text


def normalize_known_fact_phrasing(text, context_facts):
    text = str(text or "").strip()
    context_facts = context_facts or {}
    raising = str(context_facts.get("raising_method") or "")
    movement = str(context_facts.get("movement") or "")
    if "不圈养" in raising and "散养" not in raising:
        text = text.replace("散养", "不圈养")
    if "活动量比较大" in movement:
        text = text.replace("运动量足", "活动量比较大")
        text = text.replace("运动量大", "活动量比较大")
        text = text.replace("运动量很大", "活动量比较大")
        text = text.replace("运动量特别大", "活动量比较大")
        text = text.replace("运动量比较大", "活动量比较大")
    feeding = str(context_facts.get("feeding") or "")
    if feeding and "天然" not in feeding:
        text = text.replace("这些天然食物", "这些食物")
        text = text.replace("天然食物", "食物")
    return text


def sanitize_resume_tail(text, resume_sentence):
    text = str(text or "").strip()
    if not text:
        return text
    if not re.search(r"因为|所以|分不开|跟.+有关|决定|导致", text):
        return text

    topics = set((resume_sentence or {}).get("topics") or [])
    if "raising_activity" in topics:
        return "再看它平时活动这一块。"
    if "feeding" in topics:
        return "再说到平时吃的这一块。"
    if "appearance_meat" in topics:
        return "再说到收到货能看到的鸡皮和肉质。"
    if "slaughter_offal" in topics:
        return "再说到宰杀和保留的这些细节。"
    if "original_cut" in topics:
        return "再说到原切这一块。"
    if "spec_state" in topics:
        return "再看一下规格这一块。"
    if "delivery" in topics:
        return "再说到发货时效。"
    if "cooking" in topics:
        return "再说到回家怎么做。"
    return ""


def question_needs_medical_caution(question, policy_config):
    terms = (
        (policy_config.get("interaction") or {}).get("medical_escalation_terms")
        or []
    )
    q = str(question or "")
    return any(str(term) and str(term) in q for term in terms)


def remove_unneeded_professional_referral(text):
    text = str(text or "").strip()
    if not text:
        return text
    parts = re.split(r"(?<=[。！？!?])", text)
    kept = []
    for part in parts:
        if re.search(r"医生|医师|专业人士|专业医生", part):
            continue
        kept.append(part)
    return "".join(kept).strip()


def sanitize_special_population_answer(question, text):
    q = str(question or "")
    text = str(text or "").strip()
    if not re.search(r"老人|小孩|孩子|儿童|孕妇", q):
        return text
    replacements = [
        ("老人小孩当然可以吃", "老人小孩能不能吃要看个人情况"),
        ("老人小孩都能吃", "老人小孩能不能吃要看个人情况"),
        ("老人孩子当然可以吃", "老人孩子能不能吃要看个人情况"),
        ("老人孩子都能吃", "老人孩子能不能吃要看个人情况"),
        ("小孩当然可以吃", "小孩能不能吃要看个人情况"),
        ("小孩都能吃", "小孩能不能吃要看个人情况"),
        ("孕妇当然可以吃", "孕妇能不能吃要看个人情况"),
        ("孕妇都能吃", "孕妇能不能吃要看个人情况"),
        ("都适合吃", "是否适合要看个人情况"),
    ]
    for old, new in replacements:
        text = text.replace(old, new)

    risky_terms = [
        "营养很实在",
        "营养特别高",
        "营养很高",
        "营养丰富",
        "脂肪少",
        "脂肪低",
        "肉嫩",
        "不柴",
        "都适合",
        "完全适合",
    ]
    parts = re.split(r"(?<=[。！？!?])", text)
    kept = []
    for part in parts:
        if any(term in part for term in risky_terms):
            continue
        kept.append(part)
    text = "".join(kept).strip()

    if not re.search(r"个人情况|身体状况|饮食情况|有没有忌口|特殊忌口", text):
        text = "这个还是要看个人情况。" + text
    if not re.search(r"做熟|熟透|炖软|做软|切小|咀嚼", text):
        text += "烹饪时建议彻底做熟，老人孩子可以做软一些。"
    return text.strip()


def cooking_assist_fallback(context_facts):
    state = str((context_facts or {}).get("link_1_state") or "")
    help_text = str((context_facts or {}).get("cooking_help") or "")
    lines = ["这个可以作为一种做法。"]
    if state:
        lines.append("它本身是生鲜鸡，回家需要自己烹饪。")
    lines.append("炖、煮这类做法可以根据整只还是切块来调整时间，做熟再吃。")
    if help_text:
        lines.append("详细做法也可以看包装袋后面的教程入口。")
    return "".join(lines)


def sanitize_cooking_assist(text, context_facts):
    text = str(text or "").strip()
    unsupported_result_terms = [
        "汤更香",
        "汤很香",
        "汤味更浓",
        "汤更浓",
        "汤更清",
        "清亮",
        "不油腻",
        "不柴",
        "更嫩",
        "胶质",
        "营养更高",
        "营养高",
        "脂肪少",
        "脂肪低",
        "肉香",
    ]
    if any(term in text for term in unsupported_result_terms):
        return cooking_assist_fallback(context_facts)
    return text


def sanitize_dynamic_answer(text):
    text = str(text or "").strip()
    replacements = [
        ("当前商品页的剩余数量", "当前商品页的可下单状态"),
        ("商品页的剩余数量", "商品页的可下单状态"),
        ("页面显示的剩余数量", "页面显示的可下单状态"),
        ("页面的剩余数量", "页面的可下单状态"),
    ]
    for old, new in replacements:
        text = text.replace(old, new)
    return text


def reasoned_product_override(question, text, context_facts):
    q = str(question or "")
    if not re.search(r"为什么|原因|怎么会|为啥", q):
        return text
    if re.search(r"肉质.*紧实|紧实.*肉质|为什么.*紧实", q):
        movement = str((context_facts or {}).get("movement") or "")
        skin_meat = str((context_facts or {}).get("skin_meat") or "")
        raising = str((context_facts or {}).get("raising_method") or "")
        if movement and skin_meat:
            parts = [
                "一般从活动量这个角度理解，活动量比较大的鸡，肉质往往会更紧实一些。",
                "咱们这款已确认的信息是",
            ]
            known = []
            if raising:
                known.append(raising.replace("鸡", "").strip("，。 "))
            known.append(movement.replace("鸡平时", "").strip("，。 "))
            known.append(skin_meat.strip("，。 "))
            return "".join(parts) + "，".join(known) + "。"
    return text


def parse_json_object(text):
    raw = str(text or "").strip()
    raw = re.sub(r"^```(?:json)?\s*", "", raw, flags=re.I)
    raw = re.sub(r"\s*```$", "", raw)
    try:
        obj = json.loads(raw)
        if isinstance(obj, dict):
            return obj
    except Exception:
        pass
    m = re.search(r"\{[\s\S]*\}", raw)
    if m:
        obj = json.loads(m.group(0))
        if isinstance(obj, dict):
            return obj
    raise ValueError("模型没有返回可解析 JSON")


def load_timeline_v3():
    return json.loads((TIMELINE_TEST_DIR / "timeline_v3.json").read_text(encoding="utf-8"))


def timeline_sentence_at(timeline, ms):
    ms = float(ms or 0)
    arr = timeline.get("sentences") or []
    for item in arr:
        if ms >= float(item.get("play_start_ms") or 0) and ms < float(item.get("play_end_ms") or 0):
            return item
    return arr[-1] if arr else None


def next_timeline_safe_point(timeline, after_ms, min_score=80, max_after_ms=None):
    for sp in timeline.get("safe_points") or []:
        cut = float(sp.get("cut_ms") or 0)
        if cut <= float(after_ms) + 40:
            continue
        if int(sp.get("score") or 0) < int(min_score):
            continue
        if max_after_ms is not None and cut > float(max_after_ms):
            continue
        return sp
    return None


def timeline_resume_sentence(timeline, cut_ms, covered_topics):
    covered = set(covered_topics or [])
    for item in timeline.get("sentences") or []:
        if float(item.get("play_start_ms") or 0) < float(cut_ms) - 20:
            continue
        topics = set(item.get("topics") or [])
        if topics and topics.issubset(covered):
            continue
        return item
    return None


def timeline_monitor_decision(config, question, current_ms, heat_level, fact_keys, intent, timeline):
    policy_config = load_policy_config()
    monitor_policy = policy_config.get("monitor") or {}
    priority_policy = monitor_policy.get("priority") or {}
    dynamic_intents = set(
        (policy_config.get("interaction") or {}).get(
            "dynamic_intents",
            ["price_dynamic", "dynamic_unknown"],
        )
    )
    current = timeline_sentence_at(timeline, current_ms)
    payload = {
        "heat_level": heat_level,
        "question": question,
        "current_ms": round(float(current_ms), 1),
        "current_sentence": current.get("text") if current else "",
        "current_topics": current.get("topics") if current else [],
        "facts_available": bool(fact_keys),
        "fact_keys": fact_keys,
        "intent": intent,
    }
    started = time.perf_counter()
    text, model_ms, usage, cost = model_call(
        [
            {
                "role": "system",
                "content": load_policy_text(
                    "monitor_policy.md",
                    TIMELINE_MONITOR_SYSTEM,
                ),
            },
            {"role": "user", "content": json.dumps(payload, ensure_ascii=False)}
        ],
        max_tokens=140,
        temperature=0.12,
        top_p=0.5
    )
    try:
        result = parse_json_object(text)
    except Exception:
        result = {
            "decision": "PREPARE_INTERRUPT",
            "priority": "P2",
            "reason": "回答能力测试，优先安排回复"
        }

    if not fact_keys and intent in dynamic_intents:
        result["decision"] = "PREPARE_INTERRUPT"
        result["priority"] = "P1"
        result["reason"] = "购买相关问题，缺实时事实也要明确回应"
    elif not fact_keys and intent in ("reason_unknown", "unknown"):
        result["decision"] = "PREPARE_INTERRUPT"
        result["priority"] = "P2"
        result["reason"] = "测试回答能力，允许有限信息回应"

    priority = str(result.get("priority") or "P2").upper()
    if priority not in ("P1", "P2", "P3"):
        priority = "P2"
    result["priority"] = priority
    decision = str(result.get("decision") or "DEFER").upper()
    if decision not in ("PREPARE_INTERRUPT", "DEFER"):
        decision = "PREPARE_INTERRUPT"
    result["decision"] = decision

    pconf = priority_policy.get(priority) or {}
    result["min_safe_score"] = int(
        pconf.get(
            "min_safe_score",
            65 if priority == "P1" else 80,
        )
    )
    result["max_wait_ms"] = int(
        pconf.get(
            "max_wait_ms",
            9000 if priority == "P1" else (11000 if priority == "P2" else 14000),
        )
    )

    if (
        bool(monitor_policy.get("very_hot_defer_p3", True))
        and str(heat_level).upper() == "VERY_HOT"
        and priority == "P3"
    ):
        result["decision"] = "DEFER"
        result["reason"] = "高热保护主线，低价值互动延后"
    else:
        result["decision"] = str(
            monitor_policy.get("default_decision", "PREPARE_INTERRUPT")
        ).upper()
        if result["decision"] not in ("PREPARE_INTERRUPT", "DEFER"):
            result["decision"] = "PREPARE_INTERRUPT"

    result["monitor_ms"] = round((time.perf_counter() - started) * 1000, 1)
    result["usage"] = usage
    result["cost_cny"] = round(cost, 8)
    return result


def build_timeline_interaction_plan(config, data):
    question = str(data.get("question") or "").strip()
    current_ms = max(0.0, float(data.get("current_ms") or 0))
    heat_level = str(data.get("heat_level") or "WARM").upper()
    if heat_level not in ("COLD", "WARM", "HOT", "VERY_HOT"):
        heat_level = "WARM"
    if not question:
        raise ValueError("question is empty")

    timeline = load_timeline_v3()
    facts = timeline_combined_facts(config)
    policy_config = load_policy_config()
    interaction_policy = policy_config.get("interaction") or {}
    dynamic_intents = set(
        interaction_policy.get(
            "dynamic_intents",
            ["price_dynamic", "dynamic_unknown"],
        )
    )
    cooking_terms = [
        str(x) for x in interaction_policy.get("cooking_question_terms", [])
        if str(x)
    ]
    fact_keys, intent = select_timeline_fact_keys(question, facts)
    allowed_facts = {k: facts[k] for k in fact_keys if k in facts}
    allowed_topics = fact_keys_to_topics(fact_keys)
    if re.search(r"14号|小母鸡", question):
        context_facts = dict(list(facts.items())[:36])
    else:
        context_facts = {
            k: v for k, v in facts.items()
            if not k.startswith("link_14_")
        }
    is_reason_question = bool(re.search(r"为什么|原因|怎么会|为啥", question))
    if is_reason_question:
        answer_mode = "REASONED_ASSIST"
    elif allowed_facts:
        answer_mode = "VERIFIED_FACTS"
    elif intent in dynamic_intents:
        answer_mode = "DYNAMIC_CAUTION"
    elif any(term in question for term in cooking_terms):
        answer_mode = "COOKING_ASSIST"
    elif intent == "reason_unknown" or re.search(r"鸡|童子鸡|小母鸡|原切|商品|链接|发货|怎么做|怎么吃|适合|口感|味道|好吃|为什么", question):
        answer_mode = "REASONED_ASSIST"
    else:
        answer_mode = "GENERAL_CHAT"

    trace_id = uuid.uuid4().hex[:16]
    total_started = time.perf_counter()
    monitor = timeline_monitor_decision(
        config, question, current_ms, heat_level, fact_keys, intent, timeline
    )

    if monitor["decision"] != "PREPARE_INTERRUPT":
        result = {
            "trace_id": trace_id,
            "monitor": monitor,
            "question": question,
            "intent": intent,
            "fact_keys": fact_keys,
            "allowed_facts": allowed_facts,
            "interaction": None,
            "total_ms": round((time.perf_counter() - total_started) * 1000, 1)
        }
        write_trace(config["room_id"], {
            "stage": "timeline_monitor_test",
            "trace_id": trace_id,
            "question": question,
            "current_ms": round(current_ms, 1),
            "heat_level": heat_level,
            "monitor": monitor,
            "result": "DEFER",
            "created_at": time.strftime("%Y-%m-%dT%H:%M:%S")
        })
        return result

    predicted_remaining_ms = 6100.0
    predicted_ready_ms = current_ms + float(monitor.get("monitor_ms") or 0) + predicted_remaining_ms
    min_score = int(monitor.get("min_safe_score") or 80)
    max_wait_ms = int(monitor.get("max_wait_ms") or 11000)
    target = next_timeline_safe_point(
        timeline,
        predicted_ready_ms,
        min_score=min_score,
        max_after_ms=current_ms + max_wait_ms
    )
    late_risk = False
    if target is None:
        target = next_timeline_safe_point(timeline, predicted_ready_ms, min_score=min_score)
        late_risk = True
    if target is None:
        target = next_timeline_safe_point(timeline, current_ms, min_score=min_score)
        late_risk = True
    if target is None:
        raise RuntimeError("当前母带已无可用安全切点")

    cut_ms = float(target["cut_ms"])
    resume_sentence = timeline_resume_sentence(timeline, cut_ms, allowed_topics)
    resume_facts = facts_for_topics(
        facts,
        resume_sentence.get("topics") if resume_sentence else []
    )
    current_sentence = timeline_sentence_at(timeline, current_ms)
    recent_variants = get_recent_interaction_variants(
        config.get("room_id", "9"),
        intent,
        5,
    )
    future_context = []
    for item in timeline.get("sentences") or []:
        if float(item.get("play_start_ms") or 0) >= cut_ms - 20:
            future_context.append({
                "id": item.get("id"),
                "topics": item.get("topics") or [],
                "text": item.get("text") or "",
                "play_start_ms": item.get("play_start_ms"),
                "play_end_ms": item.get("play_end_ms")
            })
        if len(future_context) >= 4:
            break

    interaction_payload = {
        "question": question,
        "heat_level": heat_level,
        "intent": intent,
        "answer_mode": answer_mode,
        "expression_mode": "SEMANTIC_VARIATION",
        "recent_variants": recent_variants,
        "allowed_facts": allowed_facts,
        "context_facts": context_facts,
        "allowed_topics": allowed_topics,
        "resume_facts": resume_facts,
        "current_sentence": {
            "id": current_sentence.get("id") if current_sentence else None,
            "topics": current_sentence.get("topics") if current_sentence else [],
            "text": current_sentence.get("text") if current_sentence else ""
        },
        "planned_cut": {
            "safe_point_id": target.get("id"),
            "cut_ms": cut_ms,
            "kind": target.get("kind"),
            "score": target.get("score"),
            "left_preview": target.get("left_preview"),
            "next_preview": target.get("next_preview")
        },
        "future_mainline": future_context,
        "planned_resume": {
            "id": resume_sentence.get("id") if resume_sentence else None,
            "topics": resume_sentence.get("topics") if resume_sentence else [],
            "text": resume_sentence.get("text") if resume_sentence else ""
        }
    }

    raw, gen_ms, usage, cost = model_call(
        [
            {
                "role": "system",
                "content": load_policy_text(
                    "interaction_policy.md",
                    TIMELINE_INTERACTION_SYSTEM,
                ),
            },
            {"role": "user", "content": json.dumps(interaction_payload, ensure_ascii=False)}
        ],
        max_tokens=260,
        temperature=0.58,
        top_p=0.84
    )
    try:
        interaction = parse_json_object(raw)
    except Exception:
        if answer_mode == "VERIFIED_FACTS":
            fallback_reply = "；".join(allowed_facts.values())
        elif answer_mode == "DYNAMIC_CAUTION":
            fallback_reply = "这类信息会实时变化，具体以当前页面显示为准；如果页面没显示，可以让客服帮你确认一下。"
        elif answer_mode == "COOKING_ASSIST":
            fallback_reply = cooking_assist_fallback(context_facts)
        elif answer_mode == "REASONED_ASSIST":
            fallback_reply = "从目前确认的信息看，可以先按这些商品特点来判断，具体没有明确的数据我不说死。"
        else:
            fallback_reply = "这个问题我看到了，先给你回一下，咱们边聊边看商品。"
        interaction = {
            "reply_core": fallback_reply,
            "resume_tail": "",
            "covered_topics": allowed_topics,
            "suggested_skip_sentence_ids": []
        }

    reply_core = str(interaction.get("reply_core") or "").strip()
    resume_tail = str(interaction.get("resume_tail") or "").strip()
    if not reply_core:
        if answer_mode == "VERIFIED_FACTS":
            reply_core = "；".join(allowed_facts.values()) + "。"
        elif answer_mode == "DYNAMIC_CAUTION":
            reply_core = "这类信息会实时变化，具体以当前页面显示为准；如果页面没显示，可以让客服帮你确认一下。"
        elif answer_mode == "COOKING_ASSIST":
            reply_core = cooking_assist_fallback(context_facts)
        elif answer_mode == "REASONED_ASSIST":
            reply_core = "从目前确认的信息看，可以先按这些商品特点来判断，具体没有明确的数据我不说死。"
        else:
            reply_core = "这个问题我看到了，先给你回一下。"
    if answer_mode == "VERIFIED_FACTS":
        grounded = strip_ungrounded_risky_sentences(reply_core, allowed_facts)
        if grounded:
            reply_core = grounded
    elif answer_mode == "COOKING_ASSIST":
        reply_core = sanitize_cooking_assist(reply_core, context_facts)
    elif answer_mode == "DYNAMIC_CAUTION":
        reply_core = sanitize_dynamic_answer(reply_core)
    elif answer_mode == "REASONED_ASSIST":
        reply_core = reasoned_product_override(
            question,
            reply_core,
            context_facts,
        )
    reply_core = soften_absolute_language(reply_core)
    reply_core = normalize_known_fact_phrasing(reply_core, context_facts)
    if not question_needs_medical_caution(question, policy_config):
        reply_core = remove_unneeded_professional_referral(reply_core)
        reply_core = sanitize_special_population_answer(question, reply_core)
    if resume_tail:
        grounded_tail = strip_ungrounded_risky_sentences(resume_tail, resume_facts)
        resume_tail = soften_absolute_language(grounded_tail)
        resume_tail = sanitize_resume_tail(resume_tail, resume_sentence)
        if "movement" in resume_facts and "比较大" in resume_facts["movement"]:
            resume_tail = resume_tail.replace("特别大", "比较大")

    covered_topics = [
        x for x in (interaction.get("covered_topics") or [])
        if x in allowed_topics
    ] or allowed_topics[:]

    valid_ids = {x.get("id") for x in future_context}
    skip_ids = []
    for sid in interaction.get("suggested_skip_sentence_ids") or []:
        if sid not in valid_ids:
            continue
        sentence = next((x for x in future_context if x.get("id") == sid), None)
        if sentence and set(sentence.get("topics") or []).issubset(set(covered_topics)):
            skip_ids.append(sid)

    bridge_replaces_resume = False
    bridge_similarity = 0.0
    bridge_overlap = 0
    if resume_tail and resume_sentence and resume_sentence.get("id") in valid_ids:
        bridge_similarity = speech_surface_similarity(
            resume_tail,
            resume_sentence.get("text") or ""
        )
        bridge_overlap = longest_surface_overlap(
            resume_tail,
            resume_sentence.get("text") or ""
        )
        # If the bridge has already said the same thing as the first resumed
        # sentence, let the bridge replace that sentence instead of replaying it.
        if bridge_similarity >= 0.62 or bridge_overlap >= 9:
            if resume_sentence.get("id") not in skip_ids:
                skip_ids.append(resume_sentence.get("id"))
            bridge_replaces_resume = True

    interaction_text = (reply_core + (" " + resume_tail if resume_tail else "")).strip()
    remember_interaction_variant(
        config.get("room_id", "9"),
        intent,
        reply_core,
    )
    interaction.update({
        "reply_core": reply_core,
        "resume_tail": resume_tail,
        "interaction_text": interaction_text,
        "answer_mode": answer_mode,
        "covered_topics": covered_topics,
        "suggested_skip_sentence_ids": skip_ids,
        "bridge_replaces_resume_sentence": bridge_replaces_resume,
        "bridge_resume_similarity": bridge_similarity,
        "bridge_resume_longest_overlap": bridge_overlap,
        "planned_cut": target,
        "planned_resume_sentence": resume_sentence,
        "late_risk": late_risk,
        "generation_ms": round(gen_ms, 1),
        "usage": usage,
        "cost_cny": round(cost, 8)
    })

    result = {
        "trace_id": trace_id,
        "question": question,
        "heat_level": heat_level,
        "intent": intent,
        "fact_keys": fact_keys,
        "allowed_facts": allowed_facts,
        "monitor": monitor,
        "interaction": interaction,
        "total_ms": round((time.perf_counter() - total_started) * 1000, 1)
    }
    write_trace(config["room_id"], {
        "stage": "timeline_interaction_test",
        "trace_id": trace_id,
        "question": question,
        "current_ms": round(current_ms, 1),
        "heat_level": heat_level,
        "intent": intent,
        "fact_keys": fact_keys,
        "monitor": monitor,
        "planned_cut": target,
        "interaction_text": interaction_text,
        "covered_topics": covered_topics,
        "skip_sentence_ids": skip_ids,
        "total_ms": result["total_ms"],
        "created_at": time.strftime("%Y-%m-%dT%H:%M:%S")
    })
    return result

def interrupt(config, data):
    global LAST_MEMBER_WELCOME
    event_type = str(data.get("event_type") or "QUESTION").upper()
    text = str(data.get("text") or "").strip()
    phase = str(data.get("phase") or "OPENING")
    source = str(data.get("source") or "manual")
    spec = STAGES.get(phase, STAGES["OPENING"])
    facts = parse_facts(config.get("facts_text"))
    trace_id = uuid.uuid4().hex[:16]
    started = time.perf_counter()

    decision = "IGNORE"
    reason = ""
    fact_keys = []
    intent = "none"
    control_goal = ""
    reply = ""
    control_ms = 0.0
    usage = {}
    cost = 0.0

    if event_type == "MEMBER":
        if source == "live":
            with COOLDOWN_LOCK:
                if time.time() - LAST_MEMBER_WELCOME < 90:
                    decision = "SUPPRESS_REPEAT"
                    reason = "WELCOME_COOLDOWN"
                else:
                    LAST_MEMBER_WELCOME = time.time()
        if decision != "SUPPRESS_REPEAT":
            if phase == "CONVERSION":
                decision = "IGNORE"
                reason = "CONVERSION_PROTECTED"
            elif spec["interruptible"]:
                decision = "SOFT_INTERRUPT"
                reason = "AGGREGATED_WELCOME"
                control_goal = "最近又有一批朋友进入直播间。用一句泛化欢迎语把新进来的观众带回当前主题，不逐个点名，不拖长。"
            else:
                decision = "IGNORE"
                reason = "MAINLINE_PRIORITY"

    elif event_type == "QUESTION":
        fact_keys, intent = select_fact_keys(text, facts)
        if source == "live" and fact_keys and should_suppress_live(intent):
            decision = "SUPPRESS_REPEAT"
            reason = "INTENT_COOLDOWN"
        elif phase == "CONVERSION":
            decision = "DEFER_TO_QNA_WINDOW"
            reason = "CONVERSION_PROTECTED"
        elif fact_keys and spec["interruptible"]:
            decision = "SOFT_INTERRUPT"
            reason = "ANSWERABLE_QUESTION"
            control_goal = "直接回答这个观众问题：" + text
        elif not fact_keys:
            decision = "DEFER_HUMAN"
            reason = "FACT_NOT_AVAILABLE"
        else:
            decision = "DEFER_TO_QNA_WINDOW"
            reason = "PHASE_NOT_INTERRUPTIBLE"

    if decision == "SOFT_INTERRUPT":
        reply, control_ms, usage, cost = generate_control(config, control_goal, fact_keys, "INTERRUPT", phase=phase)
    elif decision == "DEFER_TO_QNA_WINDOW":
        reply = "先不打断当前主线，这个问题进入待回答队列。"
    elif decision == "DEFER_HUMAN":
        reply = "现有事实不足，先不自动回答，保留给人工/客服确认。"
    elif decision == "SUPPRESS_REPEAT":
        reply = "同类事件刚处理过，这次不重复打断。"
    else:
        reply = "当前不打断主线。"

    total_ms = round((time.perf_counter() - started) * 1000, 1)
    monitor = {
        "action": "HANDLE_EVENT",
        "source": source,
        "event_type": event_type,
        "decision": decision,
        "reason": reason,
        "intent": intent,
        "fact_keys": fact_keys,
        "resume_phase": phase,
        "resume_policy": "RESUME_AFTER_INTERRUPT" if decision == "SOFT_INTERRUPT" else "KEEP_MAINLINE"
    }

    write_trace(config["room_id"], {
        "stage": "interrupt",
        "trace_id": trace_id,
        "source": source,
        "phase": phase,
        "event_type": event_type,
        "event_text": text,
        "monitor_decision": decision,
        "reason": reason,
        "intent": intent,
        "fact_keys": fact_keys,
        "final_text": reply,
        "resume_phase": phase,
        "control_ms": control_ms,
        "total_ms": total_ms,
        "cost_cny": round(cost, 8),
        "created_at": time.strftime("%Y-%m-%dT%H:%M:%S")
    })

    return {
        "trace_id": trace_id,
        "monitor": monitor,
        "control_text": reply,
        "control_ms": control_ms,
        "total_ms": total_ms,
        "usage": usage,
        "cost_cny": round(cost, 8)
    }

class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        return

    def send_json(self, obj, status=200):
        raw = json.dumps(obj, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def send_wav(self, result):
        raw = result["wav"]
        self.send_response(200)
        self.send_header("Content-Type", "audio/wav")
        self.send_header("Content-Length", str(len(raw)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-TTS-Model", str(result["model"]))
        self.send_header("X-TTS-Voice", str(result["voice"]))
        self.send_header("X-TTS-Chars", str(result["chars"]))
        self.send_header("X-TTS-First-Audio-Ms", str(result["first_audio_ms"]))
        self.send_header("X-TTS-Total-Ms", str(result["total_ms"]))
        self.send_header("X-TTS-Cost-Cny", str(result["estimated_cost_cny"]))
        self.end_headers()
        self.wfile.write(raw)

    def read_json(self):
        size = int(self.headers.get("Content-Length", "0"))
        return json.loads(self.rfile.read(size).decode("utf-8"))

    def proxy_live_stream(self, room_id):
        upstream = None
        try:
            url = CORE_BASE + "/internal/v1/rooms/" + str(room_id) + "/stream"
            upstream = requests.get(
                url,
                headers={"X-Core-Token": CORE_TOKEN},
                stream=True,
                timeout=(5, 70)
            )
            if upstream.status_code != 200:
                self.send_error(upstream.status_code, upstream.text[:200])
                return

            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream; charset=utf-8")
            self.send_header("Cache-Control", "no-cache")
            self.send_header("Connection", "keep-alive")
            self.send_header("X-Accel-Buffering", "no")
            self.end_headers()
            self.wfile.write(b": connected\n\n")
            self.wfile.flush()

            for raw in upstream.iter_lines(decode_unicode=False):
                if raw is None:
                    continue
                line = raw
                if not isinstance(line, bytes):
                    line = str(line).encode("utf-8")
                self.wfile.write(line + b"\n")
                if line == b"":
                    self.wfile.flush()
        except (BrokenPipeError, ConnectionResetError):
            return
        except Exception as exc:
            try:
                payload = json.dumps({"error": str(exc)}, ensure_ascii=False).encode("utf-8")
                self.wfile.write(b"event: error\n")
                self.wfile.write(b"data: " + payload + b"\n\n")
                self.wfile.flush()
            except Exception:
                pass
        finally:
            if upstream is not None:
                upstream.close()

    def do_GET(self):
        parsed = urlparse(self.path)
        if parsed.path == "/":
            raw = HTML_PATH.read_bytes()
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)
            return
        if parsed.path == "/strategy-chat":
            raw = STRATEGY_CHAT_HTML_PATH.read_bytes()
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)
            return
        if parsed.path == "/api/strategy-chat/config":
            config = load_strategy_chat_config()
            self.send_json({
                "ok": True,
                "config": config,
                "history": load_strategy_chat_history(30),
                "effective_policy": build_effective_strategy(config),
                "model": MODEL,
            })
            return
        if parsed.path == "/api/timeline-test":
            try:
                manifest = json.loads((TIMELINE_TEST_DIR / "timeline_test_manifest_v3.json").read_text(encoding="utf-8"))
                timeline = json.loads((TIMELINE_TEST_DIR / "timeline_v3.json").read_text(encoding="utf-8"))
                self.send_json({"ok": True, "manifest": manifest, "timeline": timeline})
            except Exception as exc:
                self.send_json({"ok": False, "error": str(exc)}, 500)
            return
        if parsed.path == "/api/timeline-test/audio":
            try:
                raw = (TIMELINE_TEST_DIR / "mainline_master.wav").read_bytes()
                self.send_response(200)
                self.send_header("Content-Type", "audio/wav")
                self.send_header("Content-Length", str(len(raw)))
                self.send_header("Cache-Control", "no-store")
                self.end_headers()
                self.wfile.write(raw)
            except Exception as exc:
                self.send_json({"ok": False, "error": str(exc)}, 500)
            return
        if parsed.path == "/api/timeline-test/srt":
            try:
                raw = (TIMELINE_TEST_DIR / "mainline_master_v2.srt").read_bytes()
                self.send_response(200)
                self.send_header("Content-Type", "text/plain; charset=utf-8")
                self.send_header("Content-Length", str(len(raw)))
                self.send_header("Cache-Control", "no-store")
                self.end_headers()
                self.wfile.write(raw)
            except Exception as exc:
                self.send_json({"ok": False, "error": str(exc)}, 500)
            return
        if parsed.path == "/api/config":
            self.send_json({"ok": True, "config": load_config(), "stages": STAGES})
            return
        if parsed.path == "/api/policies":
            cfg = load_policy_config()
            strategy_text = ""
            try:
                strategy_text = (POLICY_DIR / "strategy_policy.md").read_text(encoding="utf-8")
            except Exception:
                pass
            self.send_json({
                "ok": True,
                "version": cfg.get("version", "unknown"),
                "strategy_policy": strategy_text,
                "monitor_policy": load_policy_text(
                    "monitor_policy.md",
                    TIMELINE_MONITOR_SYSTEM,
                ),
                "interaction_policy": load_policy_text(
                    "interaction_policy.md",
                    TIMELINE_INTERACTION_SYSTEM,
                ),
                "config": cfg,
            })
            return
        if parsed.path == "/api/health":
            try:
                get_api_key()
                redis_ok = True
                core_ok = False
                try:
                    redis_cmd("PING")
                except Exception:
                    redis_ok = False
                try:
                    r = requests.get(CORE_BASE + "/healthz", timeout=3)
                    core_ok = r.status_code == 200
                except Exception:
                    pass
                self.send_json({
                    "ok": True,
                    "model": MODEL,
                    "redis": redis_ok,
                    "core": core_ok,
                    "policy_version": load_policy_config().get("version", "unknown"),
                })
            except Exception as exc:
                self.send_json({"ok": False, "error": str(exc)}, 500)
            return
        if parsed.path == "/api/live-stream":
            qs = parse_qs(parsed.query)
            room_id = (qs.get("room_id") or ["9"])[0]
            self.proxy_live_stream(room_id)
            return
        self.send_error(404)

    def do_POST(self):
        try:
            if self.path == "/api/strategy-chat/config":
                body = self.read_json()
                config = load_strategy_chat_config()
                for key in ("industry_path", "l1_policy", "l2_policy", "l3_policy", "active_layer"):
                    if key in body:
                        config[key] = body[key]
                if config.get("active_layer") not in ("L1", "L2", "L3"):
                    config["active_layer"] = "L3"
                config["pending_update"] = None
                config = save_strategy_chat_config(config)
                self.send_json({
                    "ok": True,
                    "config": config,
                    "effective_policy": build_effective_strategy(config),
                })
                return
            if self.path == "/api/strategy-chat/message":
                self.send_json({
                    "ok": True,
                    "result": strategy_chat_message(self.read_json()),
                })
                return
            if self.path == "/api/strategy-chat/apply":
                config = load_strategy_chat_config()
                pending = config.get("pending_update")
                if not pending:
                    self.send_json({"ok": False, "error": "没有待应用的策略建议"}, 400)
                    return
                target = str(pending.get("target_layer") or "L3").upper()
                field = {"L1": "l1_policy", "L2": "l2_policy", "L3": "l3_policy"}.get(target)
                if not field or not pending.get("updated_policy"):
                    self.send_json({"ok": False, "error": "待应用策略无效"}, 400)
                    return
                config[field] = str(pending["updated_policy"]).strip()
                config["pending_update"] = None
                config = save_strategy_chat_config(config)
                reply = "已应用到%s。" % target
                append_strategy_chat_history("assistant", reply, {"action": "APPLIED", "target_layer": target})
                self.send_json({
                    "ok": True,
                    "reply": reply,
                    "config": config,
                    "effective_policy": build_effective_strategy(config),
                })
                return
            if self.path == "/api/strategy-chat/reset":
                try:
                    redis_cmd("DEL", STRATEGY_CHAT_CONFIG_KEY)
                    redis_cmd("DEL", STRATEGY_CHAT_HISTORY_KEY)
                except Exception:
                    pass
                config = load_strategy_chat_config()
                self.send_json({
                    "ok": True,
                    "config": config,
                    "history": [],
                    "effective_policy": build_effective_strategy(config),
                })
                return
            if self.path == "/api/config":
                body = self.read_json()
                config = load_config()
                config.update(body)
                config["durations"] = {**DEFAULT_CONFIG["durations"], **body.get("durations", {})}
                config["speak_every"] = {**DEFAULT_CONFIG["speak_every"], **body.get("speak_every", {})}
                save_config(config)
                self.send_json({"ok": True, "config": config})
                return
            if self.path == "/api/full-speech-plan":
                self.send_json({
                    "ok": True,
                    "result": generate_full_speech_plan(load_config())
                })
                return
            if self.path == "/api/timeline-interaction-plan":
                self.send_json({
                    "ok": True,
                    "result": build_timeline_interaction_plan(load_config(), self.read_json())
                })
                return
            if self.path == "/api/mainline":
                body = self.read_json()
                self.send_json({
                    "ok": True,
                    "result": mainline(
                        load_config(),
                        str(body.get("phase") or "OPENING"),
                        int(body.get("segment_index") or 0),
                        bool(body.get("resume"))
                    )
                })
                return
            if self.path == "/api/interrupt":
                self.send_json({"ok": True, "result": interrupt(load_config(), self.read_json())})
                return
            if self.path == "/api/tts":
                body = self.read_json()
                text = str(body.get("text") or "").strip()
                config = load_config()
                voice = str(body.get("voice") or config.get("tts_voice") or "Cherry")
                speech_rate = float(body.get("speech_rate") or config.get("tts_speech_rate") or 1.0)
                result = synthesize_wav(text, voice=voice, speech_rate=speech_rate)
                try:
                    write_trace(config["room_id"], {
                        "stage": "tts",
                        "trace_id": uuid.uuid4().hex[:16],
                        "tts_model": result["model"],
                        "tts_voice": result["voice"],
                        "tts_chars": result["chars"],
                        "tts_first_audio_ms": result["first_audio_ms"],
                        "tts_total_ms": result["total_ms"],
                        "tts_cost_cny": result["estimated_cost_cny"],
                        "text": text,
                        "created_at": time.strftime("%Y-%m-%dT%H:%M:%S")
                    })
                except Exception:
                    pass
                self.send_wav(result)
                return
            self.send_error(404)
        except Exception as exc:
            self.send_json({"ok": False, "error": str(exc)}, 500)

if __name__ == "__main__":
    print("直播导演循环测试台 v2: http://127.0.0.1:8767")
    ThreadingHTTPServer((HOST, PORT), Handler).serve_forever()
