package notifiers

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/grafana/grafana/pkg/bus"
	"github.com/grafana/grafana/pkg/infra/log"
	"github.com/grafana/grafana/pkg/models"
	"github.com/grafana/grafana/pkg/services/alerting"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
)

// WeComRobotNotifier is responsible for sending alert notification to WeCom group robot
type WeComRobotNotifier struct {
	NotifierBase
	Webhook      string
	UserId       string
	MobileNumber string
	log          log.Logger
}

func init() {
	alerting.RegisterNotifier(&alerting.NotifierPlugin{
		Type:        "wecom robot",
		Name:        "WeCom Robot",
		Description: "Sends notifications using WeCom group robot",
		Factory:     newWeComRobotNotifier,
		Options: []alerting.NotifierOption{
			{
				Label:        "Webhook",
				Element:      alerting.ElementTypeInput,
				InputType:    alerting.InputTypeText,
				Placeholder:  "Your WeCom Group Robot Webhook URL",
				PropertyName: "webhook",
				Required:     true,
			},
			{
				Label:        "UserId",
				Element:      alerting.ElementTypeInput,
				InputType:    alerting.InputTypeText,
				Description:  "You can enter multiple UserId using a \";\" separator",
				PropertyName: "userid",
				Required:     false,
			},
			{
				Label:        "Mobile",
				Element:      alerting.ElementTypeInput,
				InputType:    alerting.InputTypeText,
				Description:  "You can enter multiple phone number using a \";\" separator",
				PropertyName: "mobile",
				Required:     false,
			},
		},
	})
}

func newWeComRobotNotifier(model *models.AlertNotification) (alerting.Notifier, error) {
	webhook := model.Settings.Get("webhook").MustString()
	if webhook == "" {
		return nil, alerting.ValidationError{Reason: "Could not find webhook in settings"}
	}
	userId := strings.ReplaceAll(model.Settings.Get("userid").MustString(), " ", "")
	mobileNumber := strings.ReplaceAll(model.Settings.Get("mobile").MustString(), " ", "")
	return &WeComRobotNotifier{
		NotifierBase: NewNotifierBase(model),
		Webhook:      webhook,
		UserId:       userId,
		MobileNumber: mobileNumber,
		log:          log.New("alerting.notifier.wecom_robot"),
	}, nil
}

// Notify sends the alert notification to WeCom group robot
func (w *WeComRobotNotifier) Notify(evalContext *alerting.EvalContext) error {
	w.log.Info("Sending WeCom Group Robot")

	content := evalContext.GetNotificationTitle()
	content += "\n\n"

	if evalContext.Rule.State != models.AlertStateOK {
		content += "Message:\n  " + evalContext.Rule.Message + "\n\n"
	}

	for index, evt := range evalContext.EvalMatches {
		if index == 0 {
			content += "Metric:\n"
		}

		if index > 4 {
			content += "  ...\n"
			break
		}
		content += "  " + evt.Metric + "=" + strconv.FormatFloat(evt.Value.Float64, 'f', -1, 64) + "\n"
	}
	content += "\n"

	if evalContext.Error != nil {
		content += "Error:\n  " + evalContext.Error.Error() + "\n\n"
	}

	if w.NeedsImage() && evalContext.ImagePublicURL != "" {
		content += "ImageUrl:\n  " + evalContext.ImagePublicURL + "\n"
	}

	mentionedList := make([]string, 5)
	mentionedMobileList := make([]string, 5)

	if len(w.UserId) != 0 {
		for _, user := range strings.Split(w.UserId, ";") {
			mentionedList = append(mentionedList, user)
		}
	}

	if len(w.MobileNumber) != 0 {
		for _, number := range strings.Split(w.MobileNumber, ";") {
			mentionedMobileList = append(mentionedMobileList, number)
		}
	}

	body := map[string]interface{}{
		"msgtype": "text",
		"text": map[string]interface{}{
			"content":               content,
			"mentioned_list":        mentionedList,
			"mentioned_mobile_list": mentionedMobileList,
		},
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		w.log.Error("Failed to marshal body", "error", err)
		return err
	}

	msgCmd := &models.SendWebhookSync{
		Url:  w.Webhook,
		Body: string(bodyJSON),
	}

	if err := bus.DispatchCtx(evalContext.Ctx, msgCmd); err != nil {
		w.log.Error("Failed to send WeCom", "error", err)
		return err
	}

	// Push local image to wecom group
	if w.NeedsImage() && evalContext.ImagePublicURL == "" {
		var filePath string

		if _, err := os.Stat(evalContext.ImageOnDiskPath); err != nil {
			return nil
		}

		filePath = evalContext.ImageOnDiskPath

		imgFile, err := os.Open(filePath)
		defer imgFile.Close()
		if err != nil {
			return err
		}

		f, _ := ioutil.ReadAll(imgFile)

		imgBase64Str := base64.StdEncoding.EncodeToString(f)
		md5Str := fmt.Sprintf("%x", md5.Sum(f))

		imgBody := map[string]interface{}{
			"msgtype": "image",
			"image": map[string]string{
				"base64": imgBase64Str,
				"md5":    md5Str,
			},
		}

		imgBodyJSON, err := json.Marshal(imgBody)
		if err != nil {
			w.log.Error("Failed to marshal body", "error", err)
			return err
		}

		imgCmd := &models.SendWebhookSync{
			Url:  w.Webhook,
			Body: string(imgBodyJSON),
		}

		if err := bus.DispatchCtx(evalContext.Ctx, imgCmd); err != nil {
			fmt.Println("image error", err)
			w.log.Error("Failed to send WeCom", "error", err)
			return err
		}
	}

	return nil
}
