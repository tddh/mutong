package inspection

import (
	"gorm.io/gorm"

	"gitee.com/tddh/mutong/models/inspection"
)

type RuleStore struct {
	db *gorm.DB
}

func NewRuleStore(db *gorm.DB) *RuleStore {
	return &RuleStore{db: db}
}

func (s *RuleStore) List(enabledOnly bool) ([]inspection.InspectionRuleModel, error) {
	tx := s.db
	if enabledOnly {
		tx = tx.Where("enabled = ?", true)
	}
	var rules []inspection.InspectionRuleModel
	err := tx.Order("id ASC").Find(&rules).Error
	return rules, err
}

func (s *RuleStore) Get(id uint) (*inspection.InspectionRuleModel, error) {
	var r inspection.InspectionRuleModel
	if err := s.db.First(&r, id).Error; err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *RuleStore) GetByName(name string) (*inspection.InspectionRuleModel, error) {
	var r inspection.InspectionRuleModel
	if err := s.db.Where("name = ?", name).First(&r).Error; err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *RuleStore) Create(r *inspection.InspectionRuleModel) error {
	return s.db.Create(r).Error
}

func (s *RuleStore) Update(r *inspection.InspectionRuleModel) error {
	return s.db.Save(r).Error
}

func (s *RuleStore) Delete(id uint) error {
	return s.db.Delete(&inspection.InspectionRuleModel{}, id).Error
}

func (s *RuleStore) Toggle(id uint, enabled bool) error {
	return s.db.Model(&inspection.InspectionRuleModel{}).Where("id = ?", id).Update("enabled", enabled).Error
}
