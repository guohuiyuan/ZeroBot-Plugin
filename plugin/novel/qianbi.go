// Package novel 铅笔小说搜索插件
package novel

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/antchfx/htmlquery"
	"github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"golang.org/x/net/html"

	ub "github.com/FloatTech/floatbox/binary"
	"github.com/FloatTech/floatbox/file"
	"github.com/FloatTech/floatbox/web"
	ctrl "github.com/FloatTech/zbpctrl"
	"github.com/FloatTech/zbputils/control"
	"github.com/FloatTech/zbputils/ctxext"
	"github.com/FloatTech/zbputils/img/text"
)

const (
	websiteURL   = "https://www.23qb.com"
	websiteTitle = "%v 搜索结果_铅笔小说"
	errorTitle   = "出现错误！_铅笔小说"
	username     = "zerobot"
	password     = "123456"
	submit       = "%26%23160%3B%B5%C7%26%23160%3B%26%23160%3B%C2%BC%26%23160%3B"
	ua           = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/96.0.4664.110 Safari/537.36"
	loginURL     = websiteURL + "/login.php?do=submit"
	searchURL    = websiteURL + "/search.html?searchkey=%v"
	downloadURL  = websiteURL + "/modules/article/txtarticle.php?id=%v"
	detailURL    = websiteURL + "/book/%v/"
	idReg        = `/(\d+)/`
)

var (
	cachePath string
	// apikey 由账号和密码拼接而成, 例: zerobot,123456
	apikey   string
	apikeymu sync.Mutex
)

// novelInfo 小说信息结构体
type novelInfo struct {
	Title         string
	Category      string
	Author        string
	Status        string
	Description   string
	UpdateTime    string
	LatestChapter string
	WebpageURL    string
	DownloadURL   string
	CoverURL      string
	Tags          string
}

func init() {
	engine := control.AutoRegister(&ctrl.Options[*zero.Ctx]{
		DisableOnDefault: false,
		Extra:            control.ExtraFromString("novel"),
		Brief:            "铅笔小说网搜索",
		Help: "- 小说[xxx]\n" +
			"- 设置小说配置 zerobot 123456\n" +
			"- 下载小说4506\n" +
			"建议去https://www.23qb.com/ 注册一个账号, 小说下载有积分限制",
		PrivateDataFolder: "novel",
	})
	cachePath = engine.DataFolder() + "cache/"
	_ = os.MkdirAll(cachePath, 0755)
	engine.OnRegex("^小说([\u4E00-\u9FA5A-Za-z0-9]{1,25})$").SetBlock(true).Limit(ctxext.LimitByUser).
		Handle(func(ctx *zero.Ctx) {
			ctx.SendChain(message.Text("少女祈祷中......"))
			searchKey := ctx.State["regex_matched"].([]string)[1]
			searchHTML, err := search(searchKey)
			if err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
				return
			}
			doc, err := htmlquery.Parse(strings.NewReader(searchHTML))
			if err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
				return
			}
			htmlTitle := htmlquery.InnerText(htmlquery.FindOne(doc, "/html/head/title"))
			switch htmlTitle {
			case fmt.Sprintf(websiteTitle, searchKey):
				// 搜索页面 - 解析搜索结果
				novels := parseSearchResults(doc)
				if len(novels) > 0 {
					// 生成搜索结果图片
					txt := fmt.Sprintf("搜索关键词: %s\n共找到 %d 本小说\n\n", searchKey, len(novels))
					for i, novel := range novels {
						txt += fmt.Sprintf("【第%d本】\n", i+1)
						txt += fmt.Sprintf("书名: %s\n", novel.Title)
						txt += fmt.Sprintf("类型: %s\n", novel.Category)
						txt += fmt.Sprintf("标签: %s\n", novel.Tags)
						txt += fmt.Sprintf("简介: %s\n", novel.Description)
						txt += fmt.Sprintf("网页链接: %s\n", novel.WebpageURL)
						txt += fmt.Sprintf("下载地址: %s\n\n", novel.DownloadURL)
					}
					data, err := text.RenderToBase64(txt, text.FontFile, 400, 20)
					if err != nil {
						ctx.SendChain(message.Text("ERROR: ", err))
						return
					}
					if id := ctx.SendChain(message.Image("base64://" + ub.BytesToString(data))); id.ID() == 0 {
						ctx.SendChain(message.Text("ERROR: 可能被风控了"))
					}
				} else {
					text := htmlquery.InnerText(htmlquery.FindOne(doc, "//div[@id='tipss']"))
					text = strings.ReplaceAll(text, " ", "")
					text = strings.ReplaceAll(text, "本站", websiteURL)
					ctx.SendChain(message.Text(text))
				}
			case errorTitle:
				ctx.SendChain(message.Text(errorTitle))
				text := htmlquery.InnerText(htmlquery.FindOne(doc, "//div[@style='text-align: center;padding:10px']"))
				text = strings.ReplaceAll(text, " ", "")
				ctx.SendChain(message.Text(text))
			default:
				// 详情页面 - 解析小说详情
				novel := parseNovelDetail(doc)
				text := fmt.Sprintf("书名: %s\n类型: %s\n作者: %s\n状态: %s\n简介: %s\n更新时间: %s\n最新章节: %s\n网页链接: %s\n下载地址: %s\n",
					novel.Title, novel.Category, novel.Author, novel.Status,
					novel.Description, novel.UpdateTime, novel.LatestChapter,
					novel.WebpageURL, novel.DownloadURL)
				text = strings.ReplaceAll(text, "<br />", "")
				ctx.SendChain(message.Text(text))
			}
		})
	engine.OnRegex(`^设置小说配置\s(.*[^\s$])\s(.+)$`, zero.SuperUserPermission).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			regexMatched := ctx.State["regex_matched"].([]string)
			err := setAPIKey(ctx.State["manager"].(*ctrl.Control[*zero.Ctx]), regexMatched[1]+","+regexMatched[2])
			if err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
				return
			}
			ctx.SendChain(message.Text("成功设置小说配置\nusername: ", regexMatched[1], "\npassword: ", regexMatched[2]))
		})
	engine.OnRegex("^下载小说([0-9]{1,25})$").SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			regexMatched := ctx.State["regex_matched"].([]string)
			id := regexMatched[1]
			ctx.SendChain(message.Text("少女祈祷中......"))
			key := getAPIKey(ctx)
			u, p, _ := strings.Cut(key, ",")
			if u == "" {
				u = username
			}
			if p == "" {
				p = password
			}
			cookie, err := login(u, p)
			if err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
				return
			}
			detailHTML, err := detail(id)
			if err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
				return
			}
			doc, err := htmlquery.Parse(strings.NewReader(detailHTML))
			if err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
				return
			}
			htmlTitle := htmlquery.InnerText(htmlquery.FindOne(doc, "/html/head/title"))
			if htmlTitle == errorTitle {
				ctx.SendChain(message.Text("该小说不存在"))
				return
			}
			title := strings.ReplaceAll(htmlTitle, "免费在线阅读_铅笔小说", "")
			fileName := filepath.Join(cachePath, title+".txt")
			if file.IsExist(fileName) {
				ctx.UploadThisGroupFile(filepath.Join(file.BOTPATH, fileName), filepath.Base(fileName), "")
				return
			}
			data, err := download(id, cookie)
			if err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
				return
			}
			err = os.WriteFile(fileName, ub.StringToBytes(data), 0666)
			if err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
				return
			}
			ctx.UploadThisGroupFile(filepath.Join(file.BOTPATH, fileName), filepath.Base(fileName), "")
		})
}

// parseSearchResults 解析搜索结果
func parseSearchResults(doc *html.Node) []novelInfo {
	var novels []novelInfo

	// 查找搜索结果项
	items := htmlquery.Find(doc, "//div[@class='module-search-item']")
	for _, item := range items {
		novel := novelInfo{}

		// 解析标题
		titleNode := htmlquery.FindOne(item, ".//h3/a")
		if titleNode != nil {
			novel.Title = htmlquery.InnerText(titleNode)
			// 获取小说ID
			href := htmlquery.SelectAttr(titleNode, "href")
			reg := regexp.MustCompile(idReg)
			if matches := reg.FindStringSubmatch(href); len(matches) > 1 {
				id := matches[1]
				novel.WebpageURL = websiteURL + "/book/" + id + "/"
				novel.DownloadURL = websiteURL + "/modules/article/txtarticle.php?id=" + id
			}
		}

		// 解析分类
		categoryNode := htmlquery.FindOne(item, ".//a[@class='novel-serial']")
		if categoryNode != nil {
			novel.Category = htmlquery.InnerText(categoryNode)
		}

		// 解析标签
		tagNode := htmlquery.FindOne(item, ".//div[@class='tag-link']/span")
		if tagNode != nil {
			novel.Tags = htmlquery.InnerText(tagNode)
		}

		// 解析简介
		descNode := htmlquery.FindOne(item, ".//div[@class='novel-info-item']")
		if descNode != nil {
			novel.Description = strings.TrimSpace(htmlquery.InnerText(descNode))
			// 限制简介长度
			if len(novel.Description) > 100 {
				novel.Description = novel.Description[:100] + "..."
			}
		}

		// 解析封面图片
		coverNode := htmlquery.FindOne(item, ".//img[@class='lazy lazyload']")
		if coverNode != nil {
			novel.CoverURL = htmlquery.SelectAttr(coverNode, "data-src")
		}

		if novel.Title != "" {
			novels = append(novels, novel)
		}
	}

	return novels
}

// parseNovelDetail 解析小说详情页面
func parseNovelDetail(doc *html.Node) novelInfo {
	novel := novelInfo{}

	// 解析基本信息
	bookName := htmlquery.SelectAttr(htmlquery.FindOne(doc, "//meta[@property='og:novel:book_name']"), "content")
	category := htmlquery.SelectAttr(htmlquery.FindOne(doc, "//meta[@property='og:novel:category']"), "content")
	author := htmlquery.SelectAttr(htmlquery.FindOne(doc, "//meta[@property='og:novel:author']"), "content")
	status := htmlquery.SelectAttr(htmlquery.FindOne(doc, "//meta[@property='og:novel:status']"), "content")
	description := htmlquery.SelectAttr(htmlquery.FindOne(doc, "//meta[@property='og:description']"), "content")
	updateTime := htmlquery.SelectAttr(htmlquery.FindOne(doc, "//meta[@property='og:novel:update_time']"), "content")
	latestChapter := htmlquery.SelectAttr(htmlquery.FindOne(doc, "//meta[@property='og:novel:latest_chapter_name']"), "content")

	reg := regexp.MustCompile(idReg)
	id := reg.FindStringSubmatch(htmlquery.SelectAttr(htmlquery.FindOne(doc, "//meta[@property='og:novel:read_url']"), "content"))[1]
	webpageURL := websiteURL + "/book/" + id + "/"
	downloadURL := websiteURL + "/modules/article/txtarticle.php?id=" + id

	novel.Title = bookName
	novel.Category = category
	novel.Author = author
	novel.Status = status
	novel.Description = description
	novel.UpdateTime = updateTime
	novel.LatestChapter = latestChapter
	novel.WebpageURL = webpageURL
	novel.DownloadURL = downloadURL

	return novel
}

func login(username, password string) (cookie string, err error) {
	client := &http.Client{}
	loginReq, err := http.NewRequest("POST", loginURL, strings.NewReader(fmt.Sprintf("username=%s&password=%s&usecookie=86400&action=login&submit=%s", url.QueryEscape(username), url.QueryEscape(password), submit)))
	if err != nil {
		return
	}
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginReq.Header.Set("User-Agent", ua)
	loginResp, err := client.Do(loginReq)
	if err != nil {
		return
	}
	defer loginResp.Body.Close()
	for _, v := range loginResp.Cookies() {
		cookie += v.Name + "=" + v.Value + ";"
	}
	return
}

func search(searchKey string) (searchHTML string, err error) {
	data, err := web.GetData(fmt.Sprintf(searchURL, url.QueryEscape(searchKey)))
	if err != nil {
		return
	}
	searchHTML = ub.BytesToString(data)
	return
}

func detail(id string) (detailHTML string, err error) {
	data, err := web.GetData(fmt.Sprintf(detailURL, id))
	if err != nil {
		return
	}
	detailHTML = ub.BytesToString(data)
	return
}

func download(id string, cookie string) (downloadHTML string, err error) {
	data, err := web.RequestDataWithHeaders(web.NewDefaultClient(), fmt.Sprintf(downloadURL, id), "GET", func(r *http.Request) error {
		r.Header.Set("Cookie", cookie)
		r.Header.Set("User-Agent", ua)
		return nil
	}, nil)
	if err != nil {
		return
	}
	downloadHTML = ub.BytesToString(data)
	return
}

func getAPIKey(ctx *zero.Ctx) string {
	apikeymu.Lock()
	defer apikeymu.Unlock()
	if apikey == "" {
		m := ctx.State["manager"].(*ctrl.Control[*zero.Ctx])
		_ = m.GetExtra(&apikey)
		logrus.Debugln("[novel] get api key:", apikey)
	}
	return apikey
}

func setAPIKey(m *ctrl.Control[*zero.Ctx], key string) error {
	apikeymu.Lock()
	defer apikeymu.Unlock()
	apikey = key
	return m.SetExtra(apikey)
}
