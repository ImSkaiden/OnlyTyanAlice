package llm

import (
	"context"

	request "github.com/freequasar/quasar-go-genproto/proto/alice/protos/api/alicekit/events/alice_request"
)

type BiometryInfo struct {
	PersID     string `json:"pers_id"`
	YandexUID  string `json:"yandex_uid"`
	DeviceID   string `json:"device_id"`
	Status     string `json:"status"`
	Confidence string `json:"confidence"`
	Gender     string `json:"gender"`
	AgeGroup   string `json:"age_group"`
}

type biometryKey struct{}
type deviceKey struct{}

func WithBiometry(ctx context.Context, bio *BiometryInfo) context.Context {
	if bio == nil {
		return ctx
	}
	return context.WithValue(ctx, biometryKey{}, bio)
}

func GetBiometry(ctx context.Context) *BiometryInfo {
	if v, ok := ctx.Value(biometryKey{}).(*BiometryInfo); ok {
		return v
	}
	return nil
}

func WithDevice(ctx context.Context, deviceID string) context.Context {
	if deviceID == "" {
		return ctx
	}
	return context.WithValue(ctx, deviceKey{}, deviceID)
}

func GetDevice(ctx context.Context) string {
	if v, ok := ctx.Value(deviceKey{}).(string); ok {
		return v
	}
	return ""
}

func ExtractBiometry(req *request.TAliceRequestProto) *BiometryInfo {
	if req == nil {
		return nil
	}
	bio := &BiometryInfo{}
	if app := req.GetApplication(); app != nil {
		bio.DeviceID = app.GetDeviceId()
	}

	if r := req.GetRequest(); r != nil {
		if add := r.GetAdditionalOptions(); add != nil {
			if g := add.GetGuestUserOptions(); g != nil {
				bio.PersID = g.GetPersId()
				bio.YandexUID = g.GetYandexUID()
				bio.Status = g.GetStatus().String()
				bio.Confidence = g.GetMatchConfidence().String()
			}
			if bio.PersID == "" && len(add.GetUsersOptions()) > 0 {
				u := add.GetUsersOptions()[0]
				bio.PersID = u.GetPersId()
				bio.YandexUID = u.GetPuid()
				bio.Status = "Owner"
			}
		}

		if ev := r.GetEvent(); ev != nil {
			if bc := ev.GetBiometryClassification(); bc != nil {
				for _, res := range bc.GetSimple() {
					tag := res.GetTag()
					cls := res.GetClassName()
					if tag == "gender" {
						bio.Gender = cls
					} else if tag == "children" {
						bio.AgeGroup = cls
					}
				}
				if bio.Gender == "" {
					for _, sc := range bc.GetScores() {
						if sc.GetTag() == "gender" && sc.GetConfidence() > 0.5 {
							bio.Gender = sc.GetClassName()
						} else if sc.GetTag() == "children" && sc.GetConfidence() > 0.5 {
							bio.AgeGroup = sc.GetClassName()
						}
					}
				}
			}
		}
	}
	return bio
}
