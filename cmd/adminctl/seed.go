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

// demoPassage is a short Myanmar text for the demo lesson.
const demoPassage = `မောင်အောင်သည် နေ့စဉ် နံနက်စောစော အိမ်မှ ထွက်သွားပြီး ရပ်ကွက်တွင် အလုပ်သမား လုပ်သည်။ အလုပ်ပြီးဆုံး သူသည် ကော်ဖီဆိုင်တွင် ကော်ဖီနှစ်ချိုကို ဝယ်လေးသည်။ ညနေချိန်တွင် သူသည် အိမ်သို့ ပြန်ရောက်ပြီး မိသားစုနှင့် အပြင်းအနိုင်း စားသောက်သည်။ ထမင်းစားပြီးနောက် မောင်အောင်သည် စာပေစာအုပ်များကို ဖတ်သည်။ မိသားစုနှင့် ညီညာကြောင်း ပြောဆိုကြသည်။`

// demoQuestion is one question of the demo lesson.
//
// The number of options differs on purpose: the administrator chooses how many
// options a question has.
type demoQuestion struct {
	Prompt  string
	Options []string
	Correct int // one based position of the correct option
}

// demoQuestions are the questions of the demo lesson.
var demoQuestions = []demoQuestion{
	{
		Prompt:  "မောင်အောင်သည် အလုပ်သမား လုပ်သည့် နေရာမှာ မည်သနည်း။",
		Options: []string{"ရပ်ကွက်", "ကျော်ငန်း", "စာမောင်း", "အထည်ဆောင်း"},
		Correct: 1,
	},
	{
		Prompt:  "မောင်အောင် အိမ်မှ ထွက်သည့် အချိန်မှာ မည်သနည်း။",
		Options: []string{"နံနက်စောစော", "ညနေ"},
		Correct: 1,
	},
	{
		Prompt:  "အလုပ်ပြီးဆုံး မောင်အောင်က ဝယ်ယူသော ပစ္စည်းမှာ မည်သနည်း။",
		Options: []string{"ကော်ဖီနှစ်ချို", "စာပေစာအုပ်", "ရေနံချိုစည်"},
		Correct: 1,
	},
	{
		Prompt:  "ညနေချိန်တွင် မောင်အောင်သည် မိသားစုနှင့် ဘာလုပ်သည်နည်း။",
		Options: []string{"ကော်ဖီဆိုင်သို့ သွားခြင်း", "အပြင်းအနိုင်း စားသောက်ခြင်း", "စာပေဖတ်ခြင်း", "အလုပ်ဆောင်ခြင်း", "အိမ်ေအာင်ခြင်း"},
		Correct: 2,
	},
	{
		Prompt:  "ထမင်းစားပြီးနောက် မောင်အောင်သည် ဘာလုပ်သည်နည်း။",
		Options: []string{"စာပေစာအုပ်များကို ဖတ်ခြင်း", "အလုပ်ထပ်ခြင်း"},
		Correct: 1,
	},
	{
		Prompt:  "မောင်အောင်သည် မိသားစုနှင့် ညီညာကြောင်း ပြောဆိုကြသော အချိန်မှာ မည်သနည်း။",
		Options: []string{"နံနက်စောစော", "ညနေ", "နေ့လယ်", "ညပြန်"},
		Correct: 2,
	},
	{
		Prompt:  "မောင်အောင်၏ နေ့စဉ် လုပ်ဆောင်ချက်များတွင် မပါသော အချက်မှာ မည်သနည်း။",
		Options: []string{"ကော်ဖီဆိုင်တွင် ကော်ဖီရောင်းခြင်း", "ရပ်ကွက်တွင် အလုပ်လုပ်ခြင်း", "စာပေဖတ်ခြင်း", "မိသားစုနှင့် စားသောက်ခြင်း", "အိမ်တွင် အပြင်းအနိုင်းစားခြင်း"},
		Correct: 1,
	},
	{
		Prompt:  "မောင်အောင်၏ အလုပ်ခံအချိန်မှာ မည်သနည်း။",
		Options: []string{"နံနက်စောစောမှ ညနေလောက", "ညနေမှ နေ့လယ်လောက"},
		Correct: 1,
	},
	{
		Prompt:  "မောင်အောင်က ကော်ဖီကို ဝယ်သည့် နေရာမှာ မည်သနည်း။",
		Options: []string{"ကော်ဖီဆိုင်", "စျေးကွက်", "ဆိုင်ကယ်", "ပန်းချီဆိုင်", "ဓာတ်ပုံဆိုင်", "စာပေဆိုင်"},
		Correct: 1,
	},
	{
		Prompt:  "မောင်အောင်သည် ညနေ အိမ်တွင် ဘာကို လုပ်သည်နည်း။",
		Options: []string{"ကော်ဖီရောင်းခြင်း", "စာပေဖတ်ခြင်းနှင့် မိသားစုနှင့် ညီညာကြောင်းပြောခြင်း", "အလုပ်လုပ်ခြင်း", "အိမ်ပြင်ရေးခြင်း"},
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
		"နေ့စဉ် မောင်အောင်၏ ဘဝကို ဖတ်ပါ", demoPassage, true)
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
