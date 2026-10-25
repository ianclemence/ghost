package tools

// The keyword gate in turnIntentTools is written in English. An owner who
// writes in another language would otherwise get a turn without the tool the
// words clearly ask for. These are the same ideas in the languages owners write
// in: Spanish, French, German, Portuguese, Swahili, Chinese, Japanese, Korean,
// Arabic, Hindi, Russian, Turkish and Thai.
//
// Each key is the first tool of a turnIntentTools entry, and its words are
// added to that entry's keywords. Matching is a lower-cased substring test, so a
// word here must be specific enough to mean the thing; a rare false match only
// offers one more tool for a turn, never runs one. What this table misses is
// still reachable: the on-demand catalogue and find_tools let the model ask for
// a tool in any language.
var intentKeywordsI18n = map[string][]string{
	"canvas": {
		"gráfico", "diagrama", "página web", "calculadora", "juego de", "diagramme", "graphique", "page web", "calculatrice",
		"diagramm", "webseite", "taschenrechner", "jogo de", "mchoro", "mchezo", "kikokotoo",
		"图表", "流程图", "网页", "小游戏", "计算器", "原型", "グラフ", "ウェブページ", "ゲームを", "電卓", "プロトタイプ",
		"차트", "그래프", "웹페이지", "계산기", "프로토타입", "مخطط", "صفحة ويب", "آلة حاسبة", "نموذج أولي",
		"चार्ट", "ग्राफ", "वेबपेज", "कैलकुलेटर", "диаграмм", "график", "веб-страниц", "калькулятор", "прототип",
		"grafik", "diyagram", "web sayfası", "hesap makinesi", "แผนภูมิ", "ไดอะแกรม", "เครื่องคิดเลข",
	},
	"draft": {
		"correo", "responde a", "responder a", "escríbele a", "mensaje para", "invitación", "calendario", "borrador",
		"courriel", "réponds à", "écris à", "invitation", "calendrier", "brouillon",
		"antworte", "schreib an", "nachricht an", "einladung", "kalender", "entwurf",
		"escreva para", "convite", "calendário", "rascunho", "barua pepe", "mwandikie", "ujumbe kwa", "mwaliko", "kalenda", "rasimu",
		"邮件", "郵件", "回复", "发消息给", "邀请", "日历", "草稿", "メール", "返信", "招待", "カレンダー", "下書き",
		"이메일", "메일", "답장", "초대", "캘린더", "초안", "بريد", "رد على", "اكتب إلى", "دعوة", "تقويم", "مسودة",
		"ईमेल", "जवाब", "आमंत्रण", "कैलेंडर", "मसौदा", "почт", "письм", "ответь", "напиши", "приглашен", "календар", "черновик",
		"e-posta", "yanıtla", "davet", "takvim", "taslak", "อีเมล", "ตอบกลับ", "เขียนถึง", "คำเชิญ", "ปฏิทิน", "ฉบับร่าง",
	},
	"image_generate": {
		"imagen", "captura de pantalla", "capture d'écran", "bild", "foto", "bildschirmfoto", "imagem", "captura de tela", "picha",
		"图片", "照片", "截图", "图像", "画像", "写真", "スクリーンショット", "이미지", "사진", "스크린샷", "صورة", "لقطة شاشة",
		"तस्वीर", "फोटो", "स्क्रीनशॉट", "изображен", "картинк", "скриншот", "resim", "fotoğraf", "görsel", "ekran görüntüsü",
		"รูปภาพ", "ภาพถ่าย", "สกรีนช็อต",
	},
	"video_frames": {
		"vídeo", "fotogramas", "vidéo", "images clés", "videoclip", "einzelbilder", "quadros", "视频", "動画", "フレーム",
		"영상", "프레임", "فيديو", "إطارات", "वीडियो", "फ्रेम", "видео", "кадр", "kare", "วิดีโอ", "เฟรม", "fremu",
	},
	"tts": {
		"lee en voz alta", "di esto", "lis à voix haute", "dis ça", "lies vor", "sag das", "leia em voz alta", "diga isso",
		"朗读", "念给我", "说出来", "読み上げ", "声に出して", "읽어줘", "소리 내어", "اقرأ بصوت", "انطق",
		"बोलकर", "सुनाओ", "прочитай вслух", "озвучь", "скажи это", "sesli oku", "อ่านออกเสียง", "พูดออกมา", "soma kwa sauti", "sema hii",
	},
	"update": {
		"actualiza ghost", "actualizar ghost", "última versión", "hay una actualización", "nueva versión",
		"mets à jour ghost", "mise à jour", "dernière version", "nouvelle version", "aktualisiere ghost", "neueste version", "neue version",
		"atualize o ghost", "atualizar o ghost", "há uma atualização", "nova versão", "sasisha ghost", "toleo jipya", "kuna sasisho",
		"有更新", "新版本", "最新版本", "升级", "更新ghost", "更新 ghost", "アップデート", "最新バージョン", "新しいバージョン",
		"업데이트", "최신 버전", "새 버전", "تحديث", "حدّث", "أحدث إصدار", "إصدار جديد", "अपडेट", "नवीनतम संस्करण", "नया संस्करण",
		"обнови", "обновлен", "последняя версия", "новая версия", "güncelle", "son sürüm", "yeni sürüm", "อัปเดต", "เวอร์ชันล่าสุด", "เวอร์ชันใหม่",
	},
	"self_check": {
		"revísate", "comprueba tu salud", "autocomprobación", "vérifie-toi", "contrôle ta santé", "auto-vérification",
		"prüfe dich", "selbsttest", "gesundheitscheck", "verifique-se", "autoverificação", "jikague",
		"自检", "检查自己", "你还好吗", "健康检查", "自己診断", "自分をチェック", "調子はどう", "ヘルスチェック",
		"자가 진단", "스스로 점검", "افحص نفسك", "فحص ذاتي", "هل أنت بخير", "स्वयं जाँच", "स्वास्थ्य जाँच",
		"самопроверк", "проверь себя", "ты в порядке", "kendini kontrol et", "ตรวจสอบตัวเอง", "คุณโอเคไหม",
	},
	"people": {
		"mamá", "papá", "hermana", "hermano", "esposa", "esposo", "cumpleaños", "maman", "soeur", "frère", "anniversaire",
		"mutter", "vater", "schwester", "bruder", "geburtstag", "mãe", "irmã", "irmão", "aniversário",
		"mama", "baba", "dada", "kaka", "siku ya kuzaliwa", "rafiki", "妈妈", "爸爸", "姐姐", "哥哥", "生日", "朋友", "老婆", "老公",
		"お母さん", "お父さん", "誕生日", "友達", "엄마", "아빠", "생일", "친구", "أمي", "أبي", "أختي", "أخي", "عيد ميلاد", "صديق",
		"मम्मी", "पापा", "बहन", "भाई", "जन्मदिन", "दोस्त", "мама", "папа", "сестр", "брат", "день рождения", "друг",
		"annem", "babam", "kardeş", "doğum günü", "arkadaş", "แม่", "พ่อ", "พี่สาว", "พี่ชาย", "วันเกิด", "เพื่อน",
	},
	"vault": {
		"pasaporte", "seguro", "garantía", "contrato de alquiler", "carné de conducir", "caduca", "renovar",
		"passeport", "assurance", "garantie", "permis de conduire", "expire", "renouveler",
		"reisepass", "versicherung", "mietvertrag", "führerschein", "läuft ab", "verlängern",
		"passaporte", "garantia", "contrato de aluguel", "carteira de motorista", "vence", "pasipoti", "bima", "dhamana", "leseni", "mkataba",
		"护照", "签证", "保险", "保修", "租约", "驾照", "到期", "续签", "パスポート", "ビザ", "保険", "保証", "賃貸", "免許", "期限", "更新",
		"여권", "비자", "보험", "보증", "면허", "만료", "갱신", "جواز سفر", "تأشيرة", "تأمين", "ضمان", "عقد إيجار", "رخصة", "تنتهي", "تجديد",
		"पासपोर्ट", "वीज़ा", "बीमा", "वारंटी", "लाइसेंस", "नवीनीकरण", "паспорт", "виза", "страховк", "гаранти", "истекает", "продлить",
		"pasaport", "vize", "sigorta", "garanti", "ehliyet", "sona erer", "yenile", "หนังสือเดินทาง", "วีซ่า", "ประกัน", "ใบขับขี่", "หมดอายุ", "ต่ออายุ",
	},
	"phone": {
		"notificación", "mensaje de", "pasos", "sueño", "frecuencia cardíaca", "alarma", "cuando llegue", "despiértame",
		"notification", "sommeil", "fréquence cardiaque", "alarme", "quand j'arrive", "réveille-moi",
		"benachrichtigung", "nachricht von", "schritte", "schlaf", "herzfrequenz", "wecker", "wenn ich ankomme", "weck mich",
		"notificação", "mensagem de", "passos", "sono", "frequência cardíaca", "quando eu chegar", "me acorde",
		"arifa", "ujumbe kutoka", "hatua", "usingizi", "mapigo ya moyo", "kengele", "nikifika", "niamshe",
		"通知", "步数", "睡眠", "心率", "闹钟", "叫醒我", "歩数", "睡眠", "心拍", "アラーム", "着いたら", "起こして",
		"알림", "걸음", "수면", "심박", "알람", "도착하면", "깨워", "إشعار", "رسالة من", "خطوات", "نبض", "منبه", "عندما أصل", "أيقظني",
		"सूचना", "कदम", "नींद", "हृदय गति", "अलार्म", "जगाना", "уведомлен", "сообщение от", "шаги", "пульс", "будильник", "когда приду", "разбуди",
		"bildirim", "adım", "uyku", "nabız", "vardığımda", "uyandır", "การแจ้งเตือน", "ข้อความจาก", "ชีพจร", "นาฬิกาปลุก", "เมื่อถึง", "ปลุก",
	},
	"dashboard": {
		"gráfico", "cuántos", "cuánto", "promedio", "tendencia", "por mes", "hoja de cálculo", "base de datos", "ventas", "ingresos", "clientes",
		"graphique", "combien", "moyenne", "tendance", "par mois", "tableur", "base de données", "ventes", "chiffre d'affaires",
		"diagramm", "wie viele", "wie viel", "durchschnitt", "pro monat", "tabelle", "datenbank", "umsatz", "kunden",
		"quantos", "quanto", "média", "tendência", "por mês", "planilha", "banco de dados", "vendas", "receita",
		"grafu", "ngapi", "wastani", "mwenendo", "kwa mwezi", "lahajedwali", "hifadhidata", "mauzo", "mapato", "wateja",
		"多少", "总计", "平均", "趋势", "每月", "电子表格", "数据库", "销售", "收入", "客户", "仪表盘",
		"いくつ", "合計", "傾向", "月ごと", "スプレッドシート", "データベース", "売上", "顧客", "ダッシュボード",
		"몇", "합계", "평균", "추세", "월별", "스프레드시트", "데이터베이스", "매출", "고객", "대시보드",
		"كم عدد", "إجمالي", "متوسط", "اتجاه", "شهري", "جدول بيانات", "قاعدة بيانات", "مبيعات", "إيرادات", "عملاء",
		"कितने", "कुल", "औसत", "रुझान", "महीने", "स्प्रेडशीट", "डेटाबेस", "बिक्री", "राजस्व", "ग्राहक", "डैशबोर्ड",
		"сколько", "итого", "средн", "тренд", "по месяцам", "таблиц", "база данных", "продаж", "выручк", "клиент", "дашборд",
		"kaç", "toplam", "ortalama", "eğilim", "aylık", "e-tablo", "veritabanı", "satış", "gelir", "müşteri", "gösterge paneli",
		"กี่", "รวม", "เฉลี่ย", "แนวโน้ม", "รายเดือน", "สเปรดชีต", "ฐานข้อมูล", "ยอดขาย", "รายได้", "ลูกค้า", "แดชบอร์ด",
	},
	"motion": {
		"animación", "animado", "explicador", "recorrido", "animation", "animé", "explicatif", "animiert", "erklärvideo", "animação", "uhuishaji",
		"动画", "动态", "讲解", "アニメーション", "解説", "애니메이션", "동영상", "설명 영상", "رسوم متحركة", "شرح", "एनिमेशन",
		"анимац", "ролик", "animasyon", "anlatım", "แอนิเมชัน", "คลิป",
	},
	"film": {
		"dibujo", "dibuja", "caricatura", "boceto", "ilustración", "garabato", "dessin", "dessine", "croquis", "griffonnage",
		"zeichnung", "zeichne", "zeichentrick", "skizze", "kritzelei", "desenho", "desenhe", "esboço", "ilustração", "rabisco",
		"chora", "katuni", "绘画", "画一", "画个", "卡通", "素描", "插画", "涂鸦", "漫画", "描いて", "絵を", "アニメ", "スケッチ", "イラスト",
		"그려", "그림", "만화", "스케치", "일러스트", "낙서", "ارسم", "كرتون", "رسم تخطيطي", "خربشة", "चित्र", "कार्टून", "स्केच",
		"нарисуй", "рисун", "мультфильм", "набросок", "иллюстрац", "каракул", "çiz", "çizim", "çizgi film", "eskiz", "karalama",
		"วาด", "ภาพวาด", "การ์ตูน", "ภาพประกอบ",
	},
	"knowledge": {
		"libro", "capítulo", "estudiar", "examen", "curso", "lección", "conferencia", "aprender", "livre", "chapitre", "étudier", "leçon", "apprendre",
		"kapitel", "lernen", "prüfung", "lektion", "vorlesung", "capítulo", "estudar", "prova", "lição",
		"kitabu", "sura", "kusoma", "mtihani", "kozi", "somo", "kujifunza", "读书", "看书", "章节", "学习", "考试", "课程", "讲座", "复习",
		"本を読", "勉強", "試験", "授業", "講義", "学習", "챕터", "공부", "시험", "강의", "수업", "학습", "كتاب", "فصل", "دراسة", "امتحان", "دورة", "محاضرة", "تعلم",
		"किताब", "अध्याय", "पढ़ाई", "परीक्षा", "कोर्स", "पाठ", "व्याख्यान", "सीख", "книг", "глав", "учить", "экзамен", "урок", "лекци", "изуча",
		"kitap", "sınav", "öğren", "หนังสือ", "เรียน", "สอบ", "คอร์ส", "บทเรียน", "บรรยาย",
	},
	"trips": {
		"viaje", "vuelo", "reserva", "vacaciones", "itinerario", "aeropuerto", "voyage", "réservation", "vacances", "aéroport", "itinéraire",
		"reise", "flug", "buchung", "urlaub", "flughafen", "viagem", "voo", "férias", "aeroporto",
		"safari", "ndege", "likizo", "uwanja wa ndege", "旅行", "航班", "机票", "酒店", "预订", "度假", "机场", "行程",
		"フライト", "航空券", "ホテル", "予約", "休暇", "空港", "여행", "항공", "호텔", "예약", "휴가", "공항",
		"رحلة", "سفر", "طيران", "فندق", "حجز", "إجازة", "مطار", "यात्रा", "उड़ान", "होटल", "बुकिंग", "छुट्टी", "हवाई अड्डा",
		"поездк", "путешеств", "рейс", "отель", "бронирован", "отпуск", "аэропорт", "seyahat", "uçuş", "otel", "rezervasyon", "tatil", "havaalanı",
		"เดินทาง", "เที่ยวบิน", "โรงแรม", "วันหยุด", "สนามบิน", "ทริป",
	},
	"finances": {
		"gasté", "gasto", "pagué", "recibo", "presupuesto", "factura", "suscripción", "salario", "dinero", "alquiler",
		"dépensé", "dépense", "payé", "reçu", "abonnement", "loyer", "argent", "ausgegeben", "ausgaben", "bezahlt", "quittung", "miete", "geld", "rechnung", "gehalt",
		"gastei", "paguei", "orçamento", "assinatura", "aluguel", "dinheiro", "salário", "nimetumia", "gharama", "nilialipa", "risiti", "bajeti", "pesa", "mshahara", "malipo",
		"花了", "花费", "支付", "收据", "预算", "订阅", "工资", "房租", "账单", "支払", "領収書", "予算", "サブスク", "給料", "家賃", "請求書", "お金", "使った",
		"지출", "결제", "영수증", "예산", "구독", "월급", "월세", "청구서", "صرفت", "مصروف", "دفعت", "إيصال", "ميزانية", "اشتراك", "راتب", "إيجار", "فاتور",
		"खर्च", "भुगतान", "रसीद", "बजट", "सदस्यता", "वेतन", "किराया", "बिल", "पैसे", "потратил", "расход", "оплатил", "бюджет", "подписка", "зарплат", "счёт",
		"harcadım", "harcama", "ödedim", "bütçe", "abonelik", "maaş", "kira", "fatura", "ใช้จ่าย", "ใบเสร็จ", "งบประมาณ", "เงินเดือน", "ค่าเช่า", "เงิน",
	},
	"document": {
		"currículum", "carta de presentación", "propuesta", "informe", "contrato", "imprimir", "lettre de motivation", "devis", "rapport", "imprimer",
		"lebenslauf", "anschreiben", "angebot", "bericht", "drucken", "currículo", "carta de apresentação", "proposta", "relatório", "wasifu", "barua ya", "ripoti",
		"简历", "求职信", "发票", "报价", "报告", "合同", "打印", "履歴書", "カバーレター", "見積", "報告書", "契約書", "印刷",
		"이력서", "자기소개서", "견적", "보고서", "계약서", "인쇄", "سيرة ذاتية", "خطاب", "تقرير", "عقد", "طباعة",
		"रिज्यूमे", "कवर लेटर", "इनवॉइस", "प्रस्ताव", "रिपोर्ट", "अनुबंध", "प्रिंट", "резюме", "сопроводительное", "отчёт", "договор", "печат",
		"özgeçmiş", "ön yazı", "teklif", "rapor", "sözleşme", "yazdır", "เรซูเม่", "จดหมายสมัครงาน", "ใบแจ้งหนี้", "ใบเสนอราคา", "รายงาน", "สัญญา", "พิมพ์",
	},
	"device": {
		"luces", "termostato", "apaga la", "enciende la", "dispositivo", "lumière", "thermostat", "éteins", "allume", "appareil",
		"licht", "schalte", "ausschalten", "einschalten", "gerät", "luzes", "apague", "acenda", "taa za", "washa taa", "zima taa", "kifaa",
		"灯", "恒温", "关灯", "开灯", "设备", "照明", "サーモスタット", "消して", "つけて", "デバイス", "조명", "불 켜", "불 꺼", "온도 조절", "기기",
		"ضوء", "أضواء", "منظم الحرارة", "أطفئ", "شغل", "جهاز", "लाइट", "बत्ती", "थर्मोस्टेट", "बंद करो", "चालू करो", "उपकरण",
		"свет", "лампа", "термостат", "выключи", "включи", "устройств", "ışık", "kapat", "cihaz", "เปิดไฟ", "ปิดไฟ", "เทอร์โมสตัท", "อุปกรณ์",
	},
	// The first browser entry: shopping, travel, sign-in and the web in general.
	"browser_navigate": {
		"comprar", "buscar en línea", "en la web", "iniciar sesión", "vuelos", "hoteles", "entradas", "más barato", "precio de", "sitio web", "navegador",
		"acheter", "en ligne", "sur le web", "se connecter", "vols", "hôtels", "billets", "moins cher", "prix de", "site web", "navigateur",
		"kaufen", "online suchen", "im web", "anmelden", "flüge", "günstigste", "preis von", "voos", "hotéis", "ingressos", "mais barato", "preço de", "navegador",
		"nunua", "mtandaoni", "ingia", "hoteli", "tiketi", "bei nafuu", "bei ya", "kivinjari",
		"购买", "网上搜索", "登录", "最便宜", "价格", "网站", "浏览器", "購入", "ネットで検索", "ログイン", "最安", "価格", "サイト", "ブラウザ",
		"구매", "검색", "로그인", "항공권", "티켓", "최저가", "가격", "웹사이트", "브라우저", "اشتري", "شراء", "ابحث على الإنترنت", "تسجيل الدخول",
		"فنادق", "تذاكر", "الأرخص", "سعر", "موقع", "متصفح", "खरीद", "ऑनलाइन खोज", "लॉगिन", "उड़ानें", "टिकट", "सबसे सस्ता", "कीमत", "वेबसाइट", "ब्राउज़र",
		"купить", "покупк", "найди в интернете", "войти", "авиабилеты", "отели", "билеты", "дешевле", "цена", "сайт", "браузер",
		"satın al", "alışveriş", "internette ara", "giriş yap", "oteller", "bilet", "en ucuz", "fiyat", "web sitesi", "tarayıcı",
		"ซื้อ", "ค้นหาออนไลน์", "เข้าสู่ระบบ", "ตั๋วเครื่องบิน", "ตั๋ว", "ถูกที่สุด", "ราคา", "เว็บไซต์", "เบราว์เซอร์",
	},
	"browser_submit": {
		"pagar ahora", "finalizar compra", "realizar pedido", "passer la commande", "finaliser l'achat", "zur kasse", "bestellung aufgeben", "jetzt kaufen",
		"fazer pedido", "comprar agora", "lipia sasa", "kamilisha ununuzi", "结账", "下单", "立即购买", "支付订单", "購入手続き", "注文を確定", "今すぐ購入",
		"결제하기", "주문하기", "지금 구매", "إتمام الشراء", "تأكيد الطلب", "ادفع الآن", "चेकआउट", "ऑर्डर करें", "अभी खरीदें",
		"оформить заказ", "оплатить", "купить сейчас", "ödemeye geç", "siparişi ver", "hemen al", "ชำระเงิน", "สั่งซื้อ", "ซื้อเลย",
	},
	"browser_upload": {
		"subir archivo", "adjuntar", "téléverser", "joindre un fichier", "hochladen", "datei anhängen", "enviar arquivo", "anexar", "pakia faili", "ambatisha",
		"上传", "附件", "アップロード", "添付", "업로드", "첨부", "رفع ملف", "إرفاق", "अपलोड", "संलग्न", "загрузить файл", "прикрепить", "dosya yükle",
		"อัปโหลด", "แนบไฟล์",
	},
	"computer_inspect_ui": {
		"pantalla", "escritorio", "ordenador", "écran", "bureau", "ordinateur", "bildschirm", "desktop", "tela", "área de trabalho", "computador",
		"skrini", "kompyuta", "屏幕", "桌面", "电脑", "画面", "デスクトップ", "パソコン", "화면", "바탕화면", "컴퓨터", "الشاشة", "سطح المكتب", "الكمبيوتر",
		"स्क्रीन", "डेस्कटॉप", "कंप्यूटर", "экран", "рабочий стол", "компьютер", "ekran", "masaüstü", "bilgisayar", "หน้าจอ", "เดสก์ท็อป", "คอมพิวเตอร์",
	},
	"vault_items": {
		"bóveda", "tarjeta de crédito", "tarjeta de débito", "contraseña", "clave api", "mis contactos",
		"coffre-fort", "carte bancaire", "carte de crédit", "mot de passe", "clé api", "mes contacts",
		"passwort", "kreditkarte", "api-schlüssel", "meine kontakte", "cofre", "cartão de crédito", "senha", "chave de api", "meus contatos",
		"nenosiri", "kadi ya benki", "funguo la api", "密码", "密碼", "银行卡", "信用卡", "保险库", "api密钥", "我的联系人",
		"パスワード", "クレジットカード", "金庫", "apiキー", "連絡先", "비밀번호", "신용카드", "금고", "api 키", "연락처",
		"كلمة المرور", "كلمة السر", "بطاقة ائتمان", "خزنة", "पासवर्ड", "क्रेडिट कार्ड", "तिजोरी",
		"пароль", "банковская карта", "кредитная карта", "сейф", "api-ключ", "şifre", "kredi kartı", "kasa", "api anahtarı",
		"รหัสผ่าน", "บัตรเครดิต", "ห้องนิรภัย",
	},
	"pipeline": {
		"cuántos de mis", "en todos mis", "todas mis notas", "todos mis archivos", "combien de mes", "dans toutes mes", "toutes mes notes", "tous mes fichiers",
		"wie viele meiner", "in allen meinen", "alle meine notizen", "alle meine dateien", "quantos dos meus", "em todas as minhas", "todas as minhas notas",
		"todos os meus arquivos", "我的所有笔记", "我所有的文件", "我的笔记里有多少", "我的文件里有多少", "すべてのノート", "すべてのファイル",
		"내 모든 노트", "내 모든 파일", "сколько моих", "во всех моих", "все мои заметки", "все мои файлы", "madokezo yangu yote", "faili zangu zote",
		"جميع ملاحظاتي", "جميع ملفاتي", "tüm notlarım", "tüm dosyalarım", "मेरे सभी नोट्स", "मेरी सभी फ़ाइलें", "โน้ตทั้งหมดของฉัน", "ไฟล์ทั้งหมดของฉัน",
	},
}

func init() {
	applied := map[string]bool{}
	for i := range turnIntentTools {
		if len(turnIntentTools[i].tools) == 0 {
			continue
		}
		key := turnIntentTools[i].tools[0]
		if words, ok := intentKeywordsI18n[key]; ok && !applied[key] {
			turnIntentTools[i].keywords = append(turnIntentTools[i].keywords, words...)
			applied[key] = true
		}
	}
}
