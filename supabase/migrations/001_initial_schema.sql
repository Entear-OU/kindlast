-- business_profiles
CREATE TABLE business_profiles (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE NOT NULL,
  company_name TEXT NOT NULL,
  country TEXT NOT NULL DEFAULT 'Estonia',
  industry TEXT,
  employee_count INTEGER,
  processes_personal_data BOOLEAN DEFAULT true,
  data_types TEXT[],
  uses_ai_systems BOOLEAN DEFAULT false,
  ai_system_descriptions JSONB,
  third_party_processors TEXT[],
  transfers_data_outside_eu BOOLEAN DEFAULT false,
  has_dpo BOOLEAN DEFAULT false,
  has_privacy_policy BOOLEAN DEFAULT false,
  has_cookie_consent BOOLEAN DEFAULT false,
  has_breach_notification BOOLEAN DEFAULT false,
  has_dsr_process BOOLEAN DEFAULT false,
  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ DEFAULT now(),
  UNIQUE(user_id)
);
ALTER TABLE business_profiles ENABLE ROW LEVEL SECURITY;
CREATE POLICY "Users can CRUD own profiles" ON business_profiles
  USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);

-- assessments
CREATE TABLE assessments (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE NOT NULL,
  profile_id UUID REFERENCES business_profiles(id),
  type TEXT NOT NULL CHECK (type IN ('gdpr', 'ai_act')),
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'complete', 'error')),
  overall_score INTEGER,
  risk_level TEXT,
  result JSONB,
  created_at TIMESTAMPTZ DEFAULT now()
);
ALTER TABLE assessments ENABLE ROW LEVEL SECURITY;
CREATE POLICY "Users can CRUD own assessments" ON assessments
  USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);

-- findings
CREATE TABLE findings (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  assessment_id UUID REFERENCES assessments(id) ON DELETE CASCADE NOT NULL,
  user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE NOT NULL,
  category TEXT NOT NULL,
  severity TEXT NOT NULL CHECK (severity IN ('critical', 'high', 'medium', 'low', 'pass')),
  title TEXT NOT NULL,
  description TEXT NOT NULL,
  recommendation TEXT NOT NULL,
  gdpr_article TEXT,
  ai_act_article TEXT,
  is_resolved BOOLEAN DEFAULT false,
  resolved_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT now()
);
ALTER TABLE findings ENABLE ROW LEVEL SECURITY;
CREATE POLICY "Users can CRUD own findings" ON findings
  USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);

-- subscriptions (users can only read; service role manages via webhook)
CREATE TABLE subscriptions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE NOT NULL,
  stripe_customer_id TEXT,
  stripe_subscription_id TEXT,
  plan TEXT NOT NULL DEFAULT 'free' CHECK (plan IN ('free', 'premium')),
  status TEXT NOT NULL DEFAULT 'active',
  current_period_end TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT now(),
  UNIQUE(user_id)
);
ALTER TABLE subscriptions ENABLE ROW LEVEL SECURITY;
CREATE POLICY "Users can read own subscription" ON subscriptions
  FOR SELECT USING (auth.uid() = user_id);

-- Auto-create free subscription on signup
CREATE OR REPLACE FUNCTION handle_new_user()
RETURNS TRIGGER AS $$
BEGIN
  INSERT INTO subscriptions (user_id, plan, status)
  VALUES (NEW.id, 'free', 'active');
  RETURN NEW;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;

CREATE TRIGGER on_auth_user_created
  AFTER INSERT ON auth.users
  FOR EACH ROW EXECUTE FUNCTION handle_new_user();

-- Auto-update updated_at
CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN NEW.updated_at = now(); RETURN NEW; END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER set_updated_at
  BEFORE UPDATE ON business_profiles
  FOR EACH ROW EXECUTE FUNCTION update_updated_at();
