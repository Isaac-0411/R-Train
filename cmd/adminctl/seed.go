package main

import (
	"context"
	"fmt"

	"readingtrainer/internal/auth"
	"readingtrainer/internal/store"
)

const (
	// demoLessonTitle is the title of the lesson the seed command creates.
	demoLessonTitle = "နေ့စဉ်လုပ်ဆောင်ချက်"
	// demoUsername is the name of the learner account the seed command creates.
	demoUsername = "student"
	// demoPassword is the password of the learner account of the demo.
	demoPassword = "student12345"
)

// demoPassage is a short Myanmar text for the demo lesson. It is the text
// the learner reads on the lesson page.
const demoPassage = `မောင်အောင်သည် နေ့စဉ် နံနက်စောစော အိမ်မှ ထွက်သွားပြီး ရပ်ကွက်တွင် အလုပ်သမား လုပ်သည်။ အလုပ်ပြီးဆုံး သူသည် ကော်ဖီဆိုင်တွင် ကော်ဖီနှစ်ချိုကို ဝယ်လေးသည်။ ညနေချိန်တွင် သူသည် အိမ်သို့ ပြန်ရောက်ပြီး မိသားစုနှင့် အပြင်းအနိုင်း စားသောက်သည်။ ထမင်းစားပြီးနောက် မောင်အောင်သည် စာပေစာအုပ်များကို ဖတ်သည်။ မိသားစုနှင့် ညီညာကြောင်း ပြောဆိုကြသည်။`

// demoQuizPassage is the text of the question page. It tells the same story
// as the Myanmar text, but in Japanese.
const demoQuizPassage = `マウン・アウンさんは毎日、朝早く家を出て、町へ仕事に行きます。仕事のあと、喫茶店へ行き、コーヒーを二杯買います。夕方の時間、家に帰って、家族と一緒に夕食を食べます。食事のあとに、本を読みます。最後は、家族と話し合います。`

// demoQuestion is one question of the demo lesson.
//
// The number of options differs on purpose: the administrator chooses how many
// options a question has.
type demoQuestion struct {
	Prompt  string
	Options []string
	Correct int // one based position of the correct option
}

// demoQuestions are the questions of the demo lesson. They are written in
// Japanese and belong to the Japanese text of the question page.
var demoQuestions = []demoQuestion{
	{
		Prompt:  "マウン・アウンさんはいつ家を出ますか。",
		Options: []string{"朝早く", "昼過ぎ", "夕方", "夜中"},
		Correct: 1,
	},
	{
		Prompt:  "マウン・アウンさんはどこで仕事をしますか。",
		Options: []string{"町", "家", "学校", "病院", "レストラン"},
		Correct: 1,
	},
	{
		Prompt:  "仕事のあと、マウン・アウンさんは何をしますか。",
		Options: []string{"コーヒーを二杯買う", "本を読む", "昼寝する", "電話をかける", "掃除する"},
		Correct: 1,
	},
	{
		Prompt:  "コーヒーをどこで買いますか。",
		Options: []string{"喫茶店", "スーパー", "コンビニ", "本屋", "駅"},
		Correct: 1,
	},
	{
		Prompt:  "夕方の時間、マウン・アウンさんはどこにいますか。",
		Options: []string{"会社", "喫茶店", "公園", "家", "駅"},
		Correct: 4,
	},
	{
		Prompt:  "夕食は誰と一緒に食べますか。",
		Options: []string{"家族", "同僚", "友達", "先生", "ひとり"},
		Correct: 1,
	},
	{
		Prompt:  "ごはんを食べたあと、マウン・アウンさんは何をしますか。",
		Options: []string{"本を読む", "テレビを見る", "料理を作る", "水泳をする"},
		Correct: 1,
	},
	{
		Prompt:  "マウン・アウンさんは一日で何回家に帰りますか。",
		Options: []string{"一回", "二回"},
		Correct: 1,
	},
	{
		Prompt:  "次のうち、マウン・アウンさんがしないことは何ですか。",
		Options: []string{"コーヒーを買う", "本を読む", "家族と話す", "水泳をする", "電車に乗る"},
		Correct: 4,
	},
	{
		Prompt:  "マウン・アウンさんはコーヒーを何杯買いますか。",
		Options: []string{"一杯", "二杯", "三杯", "四杯"},
		Correct: 2,
	},
}

// seedDemo fills the database with a lesson, its questions and a learner
// account, so that the whole flow can be tried out locally.
//
// The command can be run again at any time, it never changes an existing
// account and never adds a second copy of the lesson.
func seedDemo(ctx context.Context, st *store.Store, adminUsername, adminPassword string) error {
	if adminPassword != "" {
		if err := seedAdmin(ctx, st, adminUsername, adminPassword); err != nil {
			return err
		}
	}

	if err := seedDemoUser(ctx, st); err != nil {
		return err
	}

	lessons, err := st.ListLessons(ctx)
	if err != nil {
		return err
	}
	for _, lesson := range lessons {
		if lesson.Title == demoLessonTitle {
			fmt.Printf("the demo lesson %q exists already, nothing was changed\n", demoLessonTitle)
			return nil
		}
	}

	lesson, err := st.CreateLesson(ctx, demoLessonTitle,
		"နေ့စဉ် မောင်အောင်၏ ဘဝကို ဖတ်ပါ", demoPassage, demoQuizPassage, true)
	if err != nil {
		return err
	}
	for _, question := range demoQuestions {
		if _, err := st.CreateQuestion(ctx, lesson.ID, question.Prompt, question.Options, question.Correct); err != nil {
			return err
		}
	}

	fmt.Printf("the demo lesson %q with %d questions was created (id %d)\n",
		lesson.Title, len(demoQuestions), lesson.ID)
	return nil
}

// seedDemoUser creates the learner account of the demo.
func seedDemoUser(ctx context.Context, st *store.Store) error {
	if _, err := st.GetUserByUsername(ctx, demoUsername); err == nil {
		fmt.Printf("the demo account %q exists already\n", demoUsername)
		return nil
	}

	hash, err := auth.HashPassword(demoPassword)
	if err != nil {
		return err
	}
	user, err := st.CreateUser(ctx, demoUsername, "Demo Learner", hash, store.RoleUser, false)
	if err != nil {
		return err
	}
	fmt.Printf("the demo account %q with the password %q was created (id %d)\n",
		user.Username, demoPassword, user.ID)
	return nil
}

// seedAdmin creates the main administrator when the database has none yet.
func seedAdmin(ctx context.Context, st *store.Store, username, password string) error {
	hasRoot, err := st.HasRootAdmin(ctx)
	if err != nil {
		return err
	}
	if hasRoot {
		fmt.Println("a main administrator exists already, nothing was changed")
		return nil
	}
	if username == "" {
		username = "admin"
	}
	if len(password) < 8 {
		return fmt.Errorf("the password of the main administrator must be at least 8 characters long")
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	user, err := st.CreateUser(ctx, username, "Local Administrator", hash, store.RoleAdmin, true)
	if err != nil {
		return err
	}
	fmt.Printf("the main administrator %q with the password %q was created (id %d)\n",
		user.Username, password, user.ID)
	return nil
}
