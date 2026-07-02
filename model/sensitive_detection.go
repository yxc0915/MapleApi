package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"

	"gorm.io/gorm"
)

type SensitiveDetectionStat struct {
	Id           int    `json:"id" gorm:"primaryKey"`
	DimensionKey string `json:"dimension_key" gorm:"size:64;uniqueIndex"`

	UserId      int    `json:"user_id" gorm:"index;default:0"`
	TokenId     int    `json:"token_id" gorm:"index;default:0"`
	TokenName   string `json:"token_name" gorm:"size:191;index;default:''"`
	ChannelId   int    `json:"channel_id" gorm:"index;default:0"`
	GroupName   string `json:"group" gorm:"column:group_name;size:191;index;default:''"`
	ModelName   string `json:"model_name" gorm:"size:191;index;default:''"`
	Trigger     string `json:"trigger" gorm:"size:32;index;default:''"`
	FlaggedItem string `json:"flagged_item" gorm:"type:text"`

	NormalCount    int64 `json:"normal_count" gorm:"default:0"`
	IllegalCount   int64 `json:"illegal_count" gorm:"default:0"`
	AllowedCount   int64 `json:"allowed_count" gorm:"default:0"`
	BypassedCount  int64 `json:"bypassed_count" gorm:"default:0"`
	ErrorOpenCount int64 `json:"error_open_count" gorm:"default:0"`
	UpdatedAt      int64 `json:"updated_at" gorm:"bigint;index;default:0"`
}

type SensitiveDetectionStatParams struct {
	UserId    int
	TokenId   int
	TokenName string
	ChannelId int
	GroupName string
	ModelName string
	Result    types.SensitiveDetectionResult
}

type SensitiveDetectionCounter struct {
	Key            string `json:"key"`
	Name           string `json:"name,omitempty"`
	NormalCount    int64  `json:"normal_count"`
	IllegalCount   int64  `json:"illegal_count"`
	AllowedCount   int64  `json:"allowed_count"`
	BypassedCount  int64  `json:"bypassed_count"`
	ErrorOpenCount int64  `json:"error_open_count"`
}

type SensitiveDetectionStatsSummary struct {
	NormalCount    int64                       `json:"normal_count"`
	IllegalCount   int64                       `json:"illegal_count"`
	AllowedCount   int64                       `json:"allowed_count"`
	BypassedCount  int64                       `json:"bypassed_count"`
	ErrorOpenCount int64                       `json:"error_open_count"`
	TopObjects     []SensitiveDetectionCounter `json:"top_objects"`
	ChannelStats   []SensitiveDetectionCounter `json:"channel_stats"`
	GroupStats     []SensitiveDetectionCounter `json:"group_stats"`
	RecentBlocked  []*Log                      `json:"recent_blocked"`
}

type SensitiveDetectionChannelOption struct {
	Id      int    `json:"id"`
	Name    string `json:"name"`
	Type    int    `json:"type"`
	Status  int    `json:"status"`
	Enabled bool   `json:"enabled"`
}

func RecordSensitiveDetectionStat(params SensitiveDetectionStatParams) {
	if DB == nil {
		return
	}
	status := params.Result.Status
	if status == "" {
		status = types.SensitiveDetectionStatusBypassed
	}
	flaggedItem := sensitiveDetectionStatItem(params.Result.Objects)
	key := sensitiveDetectionDimensionKey(params, status, flaggedItem)
	now := common.GetTimestamp()

	err := DB.Transaction(func(tx *gorm.DB) error {
		var stat SensitiveDetectionStat
		err := tx.Where("dimension_key = ?", key).First(&stat).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			stat = SensitiveDetectionStat{
				DimensionKey: key,
				UserId:       params.UserId,
				TokenId:      params.TokenId,
				TokenName:    params.TokenName,
				ChannelId:    params.ChannelId,
				GroupName:    params.GroupName,
				ModelName:    params.ModelName,
				Trigger:      params.Result.Trigger,
				FlaggedItem:  flaggedItem,
			}
		} else if err != nil {
			return err
		}

		switch status {
		case types.SensitiveDetectionStatusBlocked, types.SensitiveDetectionStatusFlagged:
			stat.IllegalCount++
		default:
			stat.NormalCount++
			switch status {
			case types.SensitiveDetectionStatusAllowed:
				stat.AllowedCount++
			case types.SensitiveDetectionStatusBypassed:
				stat.BypassedCount++
			case types.SensitiveDetectionStatusErrorOpen:
				stat.ErrorOpenCount++
			}
		}
		stat.UpdatedAt = now
		return tx.Save(&stat).Error
	})
	if err != nil {
		common.SysLog("failed to record sensitive detection stat: " + err.Error())
	}
}

func GetSensitiveDetectionStatsSummary(limit int) (*SensitiveDetectionStatsSummary, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	var stats []SensitiveDetectionStat
	if err := DB.Find(&stats).Error; err != nil {
		return nil, err
	}

	summary := &SensitiveDetectionStatsSummary{}
	objectCounters := make(map[string]*SensitiveDetectionCounter)
	channelCounters := make(map[int]*SensitiveDetectionCounter)
	groupCounters := make(map[string]*SensitiveDetectionCounter)
	channelIds := make(map[int]struct{})

	for _, stat := range stats {
		summary.NormalCount += stat.NormalCount
		summary.IllegalCount += stat.IllegalCount
		summary.AllowedCount += stat.AllowedCount
		summary.BypassedCount += stat.BypassedCount
		summary.ErrorOpenCount += stat.ErrorOpenCount

		if stat.FlaggedItem != "" && stat.IllegalCount > 0 {
			counter := objectCounters[stat.FlaggedItem]
			if counter == nil {
				counter = &SensitiveDetectionCounter{Key: stat.FlaggedItem}
				objectCounters[stat.FlaggedItem] = counter
			}
			addSensitiveDetectionCounter(counter, stat)
		}
		if stat.ChannelId != 0 {
			channelIds[stat.ChannelId] = struct{}{}
			counter := channelCounters[stat.ChannelId]
			if counter == nil {
				key := fmt.Sprintf("%d", stat.ChannelId)
				counter = &SensitiveDetectionCounter{Key: key}
				channelCounters[stat.ChannelId] = counter
			}
			addSensitiveDetectionCounter(counter, stat)
		}
		if stat.GroupName != "" {
			counter := groupCounters[stat.GroupName]
			if counter == nil {
				counter = &SensitiveDetectionCounter{Key: stat.GroupName, Name: stat.GroupName}
				groupCounters[stat.GroupName] = counter
			}
			addSensitiveDetectionCounter(counter, stat)
		}
	}

	channelNames := sensitiveDetectionChannelNames(channelIds)
	for id, counter := range channelCounters {
		counter.Name = channelNames[id]
	}

	summary.TopObjects = topSensitiveDetectionCounters(objectCounters, limit, true)
	summary.ChannelStats = topSensitiveDetectionCounters(channelCounters, limit, false)
	summary.GroupStats = topSensitiveDetectionCounters(groupCounters, limit, false)

	recent, err := GetRecentSensitiveDetectionBlockedLogs(limit)
	if err != nil {
		return nil, err
	}
	summary.RecentBlocked = recent
	return summary, nil
}

func GetRecentSensitiveDetectionBlockedLogs(limit int) ([]*Log, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	order := "created_at desc, id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("")
	}
	var logs []*Log
	err := LOG_DB.Where("sensitive_detection_status IN ?", []string{string(types.SensitiveDetectionStatusBlocked), string(types.SensitiveDetectionStatusFlagged)}).
		Order(order).
		Limit(limit).
		Find(&logs).Error
	if err != nil {
		return nil, err
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		assignDisplayLogIds(logs, 0)
	}
	return logs, nil
}

func ListSensitiveDetectionChannels() ([]SensitiveDetectionChannelOption, error) {
	var channels []Channel
	if err := DB.Select("id", "name", "type", "status", "settings").Order("id asc").Find(&channels).Error; err != nil {
		return nil, err
	}
	options := make([]SensitiveDetectionChannelOption, 0, len(channels))
	for _, channel := range channels {
		settings := parseChannelOtherSettings(channel.OtherSettings)
		options = append(options, SensitiveDetectionChannelOption{
			Id:      channel.Id,
			Name:    channel.Name,
			Type:    channel.Type,
			Status:  channel.Status,
			Enabled: settings.SensitiveDetectionEnabled,
		})
	}
	return options, nil
}

func UpdateSensitiveDetectionChannels(enabledIds []int) error {
	enabledSet := make(map[int]struct{}, len(enabledIds))
	for _, id := range enabledIds {
		if id > 0 {
			enabledSet[id] = struct{}{}
		}
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		var channels []Channel
		if err := tx.Select("id", "settings").Find(&channels).Error; err != nil {
			return err
		}
		for _, channel := range channels {
			settings := parseChannelOtherSettings(channel.OtherSettings)
			_, enabled := enabledSet[channel.Id]
			if settings.SensitiveDetectionEnabled == enabled {
				continue
			}
			settings.SensitiveDetectionEnabled = enabled
			data, err := common.Marshal(settings)
			if err != nil {
				return err
			}
			if err := tx.Model(&Channel{}).Where("id = ?", channel.Id).Update("settings", string(data)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func parseChannelOtherSettings(raw string) dto.ChannelOtherSettings {
	var settings dto.ChannelOtherSettings
	if strings.TrimSpace(raw) == "" {
		return settings
	}
	if err := common.UnmarshalJsonStr(raw, &settings); err != nil {
		return dto.ChannelOtherSettings{}
	}
	return settings
}

func sensitiveDetectionDimensionKey(params SensitiveDetectionStatParams, status types.SensitiveDetectionStatus, flaggedItem string) string {
	parts := []string{
		fmt.Sprintf("%d", params.UserId),
		fmt.Sprintf("%d", params.TokenId),
		params.TokenName,
		fmt.Sprintf("%d", params.ChannelId),
		params.GroupName,
		params.ModelName,
		params.Result.Trigger,
		string(status),
		flaggedItem,
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func sensitiveDetectionStatItem(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	runes := []rune(raw)
	if len(runes) > 512 {
		return string(runes[:512])
	}
	return raw
}

func addSensitiveDetectionCounter(counter *SensitiveDetectionCounter, stat SensitiveDetectionStat) {
	counter.NormalCount += stat.NormalCount
	counter.IllegalCount += stat.IllegalCount
	counter.AllowedCount += stat.AllowedCount
	counter.BypassedCount += stat.BypassedCount
	counter.ErrorOpenCount += stat.ErrorOpenCount
}

func topSensitiveDetectionCounters[T comparable](items map[T]*SensitiveDetectionCounter, limit int, illegalFirst bool) []SensitiveDetectionCounter {
	counters := make([]SensitiveDetectionCounter, 0, len(items))
	for _, counter := range items {
		counters = append(counters, *counter)
	}
	sort.Slice(counters, func(i, j int) bool {
		if illegalFirst && counters[i].IllegalCount != counters[j].IllegalCount {
			return counters[i].IllegalCount > counters[j].IllegalCount
		}
		leftTotal := counters[i].NormalCount + counters[i].IllegalCount
		rightTotal := counters[j].NormalCount + counters[j].IllegalCount
		if leftTotal != rightTotal {
			return leftTotal > rightTotal
		}
		return counters[i].Key < counters[j].Key
	})
	if len(counters) > limit {
		counters = counters[:limit]
	}
	return counters
}

func sensitiveDetectionChannelNames(channelIds map[int]struct{}) map[int]string {
	result := make(map[int]string, len(channelIds))
	if len(channelIds) == 0 {
		return result
	}
	ids := make([]int, 0, len(channelIds))
	for id := range channelIds {
		ids = append(ids, id)
	}
	var channels []struct {
		Id   int    `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	if err := DB.Table("channels").Select("id, name").Where("id IN ?", ids).Find(&channels).Error; err != nil {
		return result
	}
	for _, channel := range channels {
		result[channel.Id] = channel.Name
	}
	return result
}
