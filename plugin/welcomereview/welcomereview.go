// Package welcomereview 审核群自动欢迎与截图审核
package welcomereview

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"

	sql "github.com/FloatTech/sqlite"
	ctrl "github.com/FloatTech/zbpctrl"
	"github.com/FloatTech/zbputils/control"
)

// pluginConfig 单行配置(id 固定为 0).
type pluginConfig struct {
	ID            int64 `db:"id"`
	ReviewGroupID int64 `db:"review_group_id"` // 审核群(本插件生效的群)
	TargetGroupID int64 `db:"target_group_id"` // 通过审核后, 让用户加入的大群
	PassDelaySec  int64 `db:"pass_delay_sec"`  // 通过审核的延迟(秒), 默认 5
}

const (
	welcomeText = "欢迎新人！请发送b站三连+关注截图 或 抖音点赞收藏+关注截图 发送后秒审核哦！"
	warnText    = "请不要在审核群聊天哦，有问题请私聊群主或管理"

	defaultPassDelaySec = int64(5)
)

var (
	cfgMu sync.RWMutex
	cfg   pluginConfig
	db    sql.Sqlite
)

func loadCfg() pluginConfig {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return cfg
}

func updateCfgLocked(c pluginConfig) {
	cfgMu.Lock()
	cfg = c
	cfgMu.Unlock()
}

func reloadFromDB() error {
	var c pluginConfig
	err := db.Find("config", &c, "WHERE id = 0")
	if err == nil && c.ID == 0 {
		updateCfgLocked(c)
	}
	return err
}

func persistCfg(c pluginConfig) error {
	c.ID = 0
	updateCfgLocked(c)
	return db.Insert("config", &c)
}

func init() {
	engine := control.AutoRegister(&ctrl.Options[*zero.Ctx]{
		DisableOnDefault:  false,
		Brief:             "入群自动欢迎 + 截图审核",
		Help:              "- 设置审核群 <群号>\n- 设置大群 <群号>\n- 设置审核延迟 <秒数>\n- 查看审核配置",
		PrivateDataFolder: "welcomereview",
	})

	db = sql.New(engine.DataFolder() + "config.db")
	if err := db.Open(time.Hour); err != nil {
		logrus.Errorf("[welcomereview] open db error: %v", err)
	}
	if err := db.Create("config", &pluginConfig{}); err != nil {
		logrus.Errorf("[welcomereview] create config table error: %v", err)
	}
	if err := reloadFromDB(); err != nil {
		logrus.Debugf("[welcomereview] initial reload: %v", err)
	}

	// ---------- 1. 新人入群自动欢迎 ----------
	engine.OnNotice().SetBlock(false).
		Handle(func(ctx *zero.Ctx) {
			if ctx.Event.NoticeType != "group_increase" {
				return
			}
			// 跳过机器人自己入群的事件
			if ctx.Event.SelfID == ctx.Event.UserID {
				return
			}
			c := loadCfg()
			if c.ReviewGroupID == 0 || ctx.Event.GroupID != c.ReviewGroupID {
				return
			}
			ctx.SendChain(message.At(ctx.Event.UserID), message.Text(welcomeText))
		})

	// 位于审核群内的群消息
	inReviewGroup := func(ctx *zero.Ctx) bool {
		if ctx.Event.GroupID == 0 {
			return false
		}
		c := loadCfg()
		return c.ReviewGroupID != 0 && ctx.Event.GroupID == c.ReviewGroupID
	}
	// 消息中包含图片(可同时携带文字)
	hasImage := func(ctx *zero.Ctx) bool {
		for _, elem := range ctx.Event.Message {
			if elem.Type == "image" {
				return true
			}
		}
		return false
	}
	// 仅文本(无图片, 且不是空白, 不是命令)
	onlyText := func(ctx *zero.Ctx) bool {
		if hasImage(ctx) {
			return false
		}
		text := strings.TrimSpace(ctx.MessageString())
		if text == "" {
			return false
		}
		if strings.HasPrefix(text, "/") {
			return false
		}
		return true
	}

	// ---------- 2. 图片(或图片+文字) -> 撤回 + 检测中 + N 秒后通过 ----------
	engine.OnMessage(inReviewGroup, hasImage).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			c := loadCfg()
			// 撤回用户消息
			ctx.DeleteMessage(ctx.Event.MessageID)
			// 立即回复请稍等
			ctx.SendChain(message.Text("请稍等，正在检测..."))
			// 抓快照, 避免延迟期间被外部修改
			gid := ctx.Event.GroupID
			uid := ctx.Event.UserID
			target := c.TargetGroupID
			delay := c.PassDelaySec
			if delay <= 0 {
				delay = defaultPassDelaySec
			}
			go func() {
				time.Sleep(time.Duration(delay) * time.Second)
				ctx.SendGroupMessage(gid, message.Message{
					message.At(uid),
					message.Text("您已通过审核，请加大群：", target),
				})
			}()
		})

	// ---------- 3. 仅文本 -> 警告 ----------
	engine.OnMessage(inReviewGroup, onlyText).SetBlock(false).
		Handle(func(ctx *zero.Ctx) {
			ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(warnText))
		})

	// ---------- 4. 配置命令(主人权限, 仅私聊) ----------
	engine.OnRegex(`^设置审核群\s+(\d+)$`, zero.SuperUserPermission, zero.OnlyPrivate).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			gid, err := strconv.ParseInt(ctx.State["regex_matched"].([]string)[1], 10, 64)
			if err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
				return
			}
			c := loadCfg()
			c.ReviewGroupID = gid
			if err := persistCfg(c); err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
				return
			}
			ctx.SendChain(message.Text("已设置审核群：", gid))
		})

	engine.OnRegex(`^设置大群\s+(\d+)$`, zero.SuperUserPermission, zero.OnlyPrivate).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			gid, err := strconv.ParseInt(ctx.State["regex_matched"].([]string)[1], 10, 64)
			if err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
				return
			}
			c := loadCfg()
			c.TargetGroupID = gid
			if err := persistCfg(c); err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
				return
			}
			ctx.SendChain(message.Text("已设置大群：", gid))
		})

	engine.OnRegex(`^设置审核延迟\s+(\d+)$`, zero.SuperUserPermission, zero.OnlyPrivate).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			sec, err := strconv.ParseInt(ctx.State["regex_matched"].([]string)[1], 10, 64)
			if err != nil || sec <= 0 {
				ctx.SendChain(message.Text("延迟必须为正整数"))
				return
			}
			c := loadCfg()
			c.PassDelaySec = sec
			if err := persistCfg(c); err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
				return
			}
			ctx.SendChain(message.Text("已设置审核延迟：", sec, " 秒"))
		})

	engine.OnFullMatch("查看审核配置", zero.SuperUserPermission, zero.OnlyPrivate).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			c := loadCfg()
			ctx.SendChain(message.Text(
				"审核群: ", c.ReviewGroupID, "\n",
				"大群: ", c.TargetGroupID, "\n",
				"延迟秒数: ", c.PassDelaySec, "\n",
				"Tips: 在审核群私聊 bot 发送 设置审核群 <群号> / 设置大群 <群号> 进行配置",
			))
		})
}
