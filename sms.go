package main

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/golang/glog"
)

type Sender interface {
	Send() error
}

func sendcode(sms *SMS) error {

	var s Sender
	var vendor = sms.Config.Vendor
	//startswitch:
	switch vendor {
	case "alidayu":
		s = &Alidayu{sms: sms}
	case "yuntongxun":
		s = &Yuntongxun{sms: sms}

		//	case "auto":

		//		var names []string
		//		for name, _ := range config.Vendors {
		//			names = append(names, name)
		//		}
		//		n := rand.Intn(len(names))
		//		vendor = names[n]
		//		goto startswitch

	default:
		log.Fatal("您设置的短信服务商有误")
	}

	return s.Send()
}

type SMS struct {
	Mobile      string
	Code        string
	Uid         string
	serviceName string
	Config      ServiceConfig
	ConfigisOK  bool
	mu          sync.Mutex
	NowTime     time.Time
}

func NewSms() *SMS {
	nowTime := time.Now()
	sms := &SMS{NowTime: nowTime}
	return sms
}

//设置服务配置文件
func (sms *SMS) SetServiceConfig(serviceName string) *SMS {
	sms.mu.Lock()
	defer sms.mu.Unlock()
	sms.Config, sms.ConfigisOK = config.ServiceList[serviceName]
	if sms.ConfigisOK {
		sms.serviceName = serviceName
	}
	return sms
}

//  归属地规则校验
func (sms *SMS) checkArea() error {

	if len(sms.Config.Allowcity) < 1 { //没有启用
		return nil
	}

	area, err := SMSModel.GetMobileArea()
	if err != nil {
		return err
	}

	var Allow = false
	for _, citycode := range sms.Config.Allowcity {
		if strings.Contains(area, citycode) {
			Allow = true //允许发送sms
			break
		}
	}

	if !Allow {
		return fmt.Errorf(config.Errormsg["err_allow_areacode"], strings.Join(sms.Config.Allowcity, ","))
	}

	return nil
}

func (sms *SMS) checkhold() error {

	sendTime, err := SMSModel.GetSendTime()
	if err != nil {
		return nil
	}

	if sms.NowTime.Unix()-sendTime < 60 { //发送间隔不能小于60秒
		return fmt.Errorf(config.Errormsg["err_per_minute_send_num"])
	}

	sendMax, err := SMSModel.GetTodaySendNums()
	if err != nil {
		return nil
	}

	if sendMax >= sms.Config.MaxSendNums {
		return fmt.Errorf(config.Errormsg["err_per_day_max_send_nums"], sms.Config.MaxSendNums)
	}

	return nil
}

/**
当前模式  1：只有手机号对应的uid存在时才能发送，2：只有uid不存在时才能发送，3：不管uid是否存在都发送
**/
func (sms *SMS) currModeok() error {

	_, err := SMSModel.GetSmsUid()

	switch mode := sms.Config.Mode; mode {
	case 0x01:
		if err == nil {
			return nil
		}
		return fmt.Errorf(config.Errormsg["err_model_not_ok1"], sms.Mobile)
	case 0x02:
		if err != nil {
			return nil
		}
		return fmt.Errorf(config.Errormsg["err_model_not_ok2"], sms.Mobile)
	case 0x03:
		return nil
	}

	return fmt.Errorf("请正确配置config中的mode参数")
}

//保存数据
func (sms *SMS) save() {

	SMSModel.SetSendTime()

	nums, _ := SMSModel.GetTodaySendNums()

	atomic.AddUint64(&nums, 1) //原子操作+1

	SMSModel.SetTodaySendNums(nums)

	SMSModel.SetSmsCode()
}

//发送短信
func (sms *SMS) Send(mobile string) error {
	if !sms.ConfigisOK {
		return fmt.Errorf("(%s)服务配置不存在", sms.serviceName)
	}

	sms.Mobile = mobile

	//手机验证码
	sms.Code = makeCode()

	if err := VailMobile(sms.Mobile); err != nil {
		return err
	}
	if err := sms.checkArea(); err != nil {
		return err
	}
	if err := sms.checkhold(); err != nil {
		return err
	}
	if err := sms.currModeok(); err != nil {
		return err
	}
	if err := sendcode(sms); err != nil {

		//发送失败 callback
		AddCallbackTask(*sms, "Failed")
		return err
	}

	//保存记录
	sms.save()

	//发送成功 callback
	AddCallbackTask(*sms, "Success")

	return nil
}

func (sms *SMS) CheckCode(mobile, code string) error {
	if !sms.ConfigisOK {
		return fmt.Errorf("(%s)服务配置不存在", sms.serviceName)
	}

	sms.Mobile = mobile
	sms.Code = code

	if err := VailMobile(sms.Mobile); err != nil {
		return err
	}

	if err := VailCode(sms.Code); err != nil {
		return err
	}

	oldcode, validtime, _ := SMSModel.GetSmsCode()

	if sms.Code != oldcode {
		return fmt.Errorf(config.Errormsg["err_code_not_ok"], sms.Code)
	}

	if sms.NowTime.Unix() > validtime {
		time1 := time.Unix(validtime, 0)
		return fmt.Errorf(config.Errormsg["err_vailtime_not_ok"], time.Since(time1).String())

	}

	//验证成功时 callback
	AddCallbackTask(*sms, "Checkok")

	return nil
}

func (sms *SMS) SetUid(mobile, uid string) error {
	if !sms.ConfigisOK {
		return fmt.Errorf("(%s)服务配置不存在", sms.serviceName)
	}

	sms.Mobile = mobile
	sms.Uid = uid

	if err := VailMobile(sms.Mobile); err != nil {
		return err
	}

	if err := VailUid(sms.Uid); err != nil {
		return err
	}

	SMSModel.SetSmsUid()

	return nil
}

func (sms *SMS) DelUid(mobile, uid string) error {
	if !sms.ConfigisOK {
		return fmt.Errorf("(%s)服务配置不存在", sms.serviceName)
	}

	sms.Mobile = mobile
	sms.Uid = uid

	if err := VailMobile(sms.Mobile); err != nil {
		return err
	}
	if err := VailUid(sms.Uid); err != nil {
		return err
	}

	olduid, err := SMSModel.GetSmsUid()

	if err != nil {
		return fmt.Errorf(config.Errormsg["err_not_uid"], sms.Mobile, sms.Uid)
	}
	if olduid != uid {
		return fmt.Errorf(config.Errormsg["err_not_uid"], sms.Mobile, sms.Uid)
	}

	SMSModel.DelSmsUid()
	return nil
}

func (sms *SMS) Info(mobile string) (map[string]interface{}, error) {
	if !sms.ConfigisOK {
		return nil, fmt.Errorf("(%s)服务配置不存在", sms.serviceName)
	}
	sms.Mobile = mobile

	info := make(map[string]interface{})
	info["mobile"] = sms.Mobile
	info["service"] = sms.serviceName
	info["areacode"], _ = SMSModel.GetMobileArea()
	info["lastsendtime"], _ = SMSModel.GetSendTime()
	info["sendnums"], _ = SMSModel.GetTodaySendNums()
	info["smscode"], info["smscodevalidtime"], _ = SMSModel.GetSmsCode()

	info["uid"], _ = SMSModel.GetSmsUid()
	return info, nil
}
