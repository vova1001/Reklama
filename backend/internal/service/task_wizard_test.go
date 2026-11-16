package service

import (
	"context"
	"testing"

	"ohvat/backend/internal/domain"
)

func wizardTask() TaskInput {
	in := fullTask()
	in.Details = domain.TaskDetails{
		BizType: "offline",
		Goals: []domain.TaskGoal{
			{Goal: domain.GoalViews, Target: 5000, RewardKopecks: 300000},
			{Goal: domain.GoalClicks, Target: 100, RewardKopecks: 150000, Link: "ПРОМО2026"},
			{Goal: domain.GoalBarter},
		},
		Audience:     domain.TaskAudience{AgeFrom: 18, AgeTo: 35, Gender: "any", Language: "Русский"},
		MinFollowers: 1000, Requirements: "Вертикальное видео", Script: "Показать интерьер", Barter: "Ужин на двоих", ResponseDays: 3,
	}
	return in
}

func TestTaskWizardMultipleGoals(t *testing.T) {
	e := newListingEnv(ListingLimits{})
	ctx := context.Background()
	got, err := e.tasks.Create(ctx, biz, wizardTask())
	if err != nil {
		t.Fatal(err)
	}
	if got.Goal != domain.GoalViews || got.MinViews != 5000 || got.RewardKopecks != 300000 {
		t.Fatalf("совместимость основной цели: %+v", got)
	}
	if len(got.Details.Goals) != 3 || got.Details.Goals[1].Target != 100 || got.Details.Barter != "Ужин на двоих" {
		t.Fatalf("условия целей: %+v", got.Details)
	}
	if _, err := e.tasks.Submit(ctx, biz, got.ID); err != nil {
		t.Fatal(err)
	}
}

func TestTaskWizardValidatesEachGoal(t *testing.T) {
	e := newListingEnv(ListingLimits{})
	in := wizardTask()
	in.Details.BizType = ""
	in.Details.Goals[0].Target = 0
	in.Details.Goals[1].RewardKopecks = 0
	in.Details.Goals[1].Link = ""
	in.Details.Barter = ""
	got, err := e.tasks.Create(context.Background(), biz, in)
	if err != nil {
		t.Fatal(err)
	}
	f := fields(t, func() error { _, err := e.tasks.Submit(context.Background(), biz, got.ID); return err }())
	for _, k := range []string{"biz", "goal.views", "reward.clicks", "link.clicks", "barter"} {
		if f[k] == "" {
			t.Errorf("нет ошибки %s: %v", k, f)
		}
	}
}

func TestTaskWizardShapeLimits(t *testing.T) {
	e := newListingEnv(ListingLimits{})
	in := wizardTask()
	in.Details.Goals = append(in.Details.Goals, in.Details.Goals[0])
	in.Details.Audience.AgeFrom = 60
	in.Details.Audience.AgeTo = 20
	in.Details.Goals[0].PaymentMode = "hourly"
	_, err := e.tasks.Create(context.Background(), biz, in)
	f := fields(t, err)
	if f["goals"] == "" || f["audience"] == "" {
		t.Fatalf("ограничения: %v", f)
	}
}
