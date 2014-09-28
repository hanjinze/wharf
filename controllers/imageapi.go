package controllers

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/astaxie/beego"

	"github.com/dockercn/docker-bucket/models"
	"github.com/dockercn/docker-bucket/utils"
)

type ImageAPIController struct {
	beego.Controller
}

func (i *ImageAPIController) URLMapping() {
	i.Mapping("GetImageJSON", i.GetImageJSON)
	i.Mapping("PutImageJson", i.PutImageJson)
	i.Mapping("PutImageLayer", i.PutImageLayer)
	i.Mapping("PutChecksum", i.PutChecksum)
	i.Mapping("GetImageAncestry", i.GetImageAncestry)
	i.Mapping("GetImageLayer", i.GetImageLayer)
}

func (this *ImageAPIController) Prepare() {
	beego.Debug("[" + this.Ctx.Request.Method + "] " + this.Ctx.Request.URL.String())

	//相应 docker api 命令的 Controller 屏蔽 beego 的 XSRF ，避免错误。
	this.EnableXSRF = false

	this.Ctx.Output.Context.ResponseWriter.Header().Set("Content-Type", "application/json;charset=UTF-8")
	this.Ctx.Output.Context.ResponseWriter.Header().Set("X-Docker-Registry-Version", beego.AppConfig.String("docker::Version"))
	this.Ctx.Output.Context.ResponseWriter.Header().Set("X-Docker-Registry-Config", beego.AppConfig.String("docker::Config"))
	this.Ctx.Output.Context.ResponseWriter.Header().Set("X-Docker-Encrypt", beego.AppConfig.String("docker::Encrypt"))

	beego.Debug(this.Ctx.Request.Header)

	//检查 Basic Auth 的认证。
	if len(this.Ctx.Input.Header("Authorization")) == 0 {
		this.Ctx.Output.Context.Output.SetStatus(http.StatusUnauthorized)
		this.Ctx.Output.Context.Output.Body([]byte("{\"错误\":\"没有找到 Authorization 的认证信息\"}"))
		this.StopRun()
	} else {
		beego.Debug("Authorization: " + this.Ctx.Input.Header("Authorization"))
		//检查是否 Basic Auth
		if strings.Index(this.Ctx.Input.Header("Authorization"), "Basic") == -1 {
			//非 Basic Auth ，检查 Token
			if strings.Index(this.Ctx.Input.Header("Authorization"), "Token") == -1 {
				this.Ctx.Output.Context.Output.SetStatus(http.StatusUnauthorized)
				this.Ctx.Output.Context.Output.Body([]byte("{\"错误\":\"在 HTTP Header Authorization 中没有找到 Basic Auth 和 Token 信息\"}"))
				this.StopRun()
			}

			r, _ := regexp.Compile(`Token (?P<token>\w+)`)
			tokens := r.FindStringSubmatch(this.Ctx.Input.Header("Authorization"))
			_, token := tokens[0], tokens[1]

			beego.Debug("[Token in Header]" + token)

			t := this.GetSession("token")

			if token != t {
				this.Ctx.Output.Context.Output.SetStatus(http.StatusUnauthorized)
				this.Ctx.Output.Context.Output.Body([]byte("{\"错误\":\"HTTP Header 中的 Token 和 Session 的 Token 不同\"}"))
				this.StopRun()
			}

		} else {
			//Decode Basic Auth 进行用户的判断
			username, passwd, err := utils.DecodeBasicAuth(this.Ctx.Input.Header("Authorization"))

			if err != nil {
				this.Ctx.Output.Context.Output.SetStatus(http.StatusUnauthorized)
				this.Ctx.Output.Context.Output.Body([]byte("{\"错误\":\"解码 Authorization 的 Basic Auth 信息错误\"}"))
				this.StopRun()
			}

			user := new(models.User)
			has, err := user.Get(username, passwd)
			if err != nil {
				//查询用户数据失败，返回 401 错误
				this.Ctx.Output.Context.Output.SetStatus(http.StatusUnauthorized)
				this.Ctx.Output.Context.Output.Body([]byte("{\"错误\":\"在数据库中查询用户数据时出现数据库错误\"}"))
				this.StopRun()
			}

			if has == true {
				//查询到用户数据，在以下的 Action 处理函数中使用 this.Data["user"]
				//TODO 这里需要根据数据库特点改为存储 Key 么？
				this.Data["user"] = user
			} else {
				//没有查询到用户数据
				this.Ctx.Output.Context.Output.SetStatus(http.StatusForbidden)
				this.Ctx.Output.Context.Output.Body([]byte("{\"错误\":\"没有在数据库中找到用户数据\"}"))
				this.StopRun()
			}
		}
	}
}

//在 Push 的流程中，docker 客户端会先调用 GET /v1/images/:image_id/json 向服务器检查是否已经存在 JSON 信息。
//如果存在了 JSON 信息，docker 客户端就认为是已经存在了 layer 数据，不再向服务器 PUT layer 的 JSON 信息和文件了。
//如果不存在 JSON 信息，docker 客户端会先后执行 PUT /v1/images/:image_id/json 和 PUT /v1/images/:image_id/layer 。
func (this *ImageAPIController) GetImageJSON() {
	if this.GetSession("access") == "write" || this.GetSession("access") == "read" {
		//加密签名
		sign := ""
		if len(string(this.Ctx.Input.Header("X-Docker-Sign"))) > 0 {
			sign = string(this.Ctx.Input.Header("X-Docker-Sign"))
		}

		//TODO 检查 imageID 的合法性
		imageId := string(this.Ctx.Input.Param(":image_id"))

		image := new(models.Image)
		has, err := image.GetPushed(imageId, sign, true, true)

		if err != nil {
			this.Ctx.Output.Context.Output.SetStatus(http.StatusBadRequest)
			this.Ctx.Output.Context.Output.Body([]byte("{\"错误\":\"搜索 Image 错误\"}"))
			this.StopRun()
		}

		if has == true {
			var json, checksum string

			if json, err = image.GetJSON(imageId, sign, true, true); err != nil {
				this.Ctx.Output.Context.Output.SetStatus(http.StatusBadRequest)
				this.Ctx.Output.Context.Output.Body([]byte("{\"错误\":\"搜索 Image 的 JSON 数据错误\"}"))
				this.StopRun()
			}

			if checksum, err = image.GetChecksum(imageId, sign, true, true); err != nil {
				this.Ctx.Output.Context.Output.SetStatus(http.StatusBadRequest)
				this.Ctx.Output.Context.Output.Body([]byte("{\"错误\":\"搜索 Image 的 Checksum 数据错误\"}"))
				this.StopRun()
			}

			this.Ctx.Output.Context.ResponseWriter.Header().Set("X-Docker-Checksum", checksum)
			this.Ctx.Output.Context.Output.SetStatus(http.StatusOK)
			this.Ctx.Output.Context.Output.Body([]byte(json))
			this.StopRun()
		} else {
			this.Ctx.Output.Context.Output.SetStatus(http.StatusNotFound)
			this.Ctx.Output.Context.Output.Body([]byte("{\"error\":\"没有找到 Image 数据\"}"))
			this.StopRun()
		}
	} else {
		this.Ctx.Output.Context.Output.SetStatus(http.StatusUnauthorized)
		this.Ctx.Output.Context.Output.Body([]byte("{\"error\":\"没有访问 Image 数据的权限\"}"))
		this.StopRun()
	}
}

//向数据库写入 Layer 的 JSON 数据
func (this *ImageAPIController) PutImageJson() {

}

//向本地硬盘写入 Layer 的文件
func (this *ImageAPIController) PutImageLayer() {

}

func (this *ImageAPIController) PutChecksum() {

}

func (this *ImageAPIController) GetImageAncestry() {

}

func (this *ImageAPIController) GetImageLayer() {

}
