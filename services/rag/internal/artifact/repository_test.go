package artifact

import (
	"context"
	"testing"
)

func TestInMemoryProcessorRepository_GetBySlug(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryProcessorRepository()

	// Add a test processor
	testProcessor := ProcessorProfileData{
		Name:              "Test Processor",
		Slug:              "test-processor",
		Category:          "testing",
		Headquarters:      "EU",
		DataCategories:    []string{"test_data"},
		ProcessingPurposes: []string{"testing"},
		DataLocations:     []string{"eu"},
		TransferMechanism: "none_required",
		DPAStatus:         "in_place",
		DPAURL:            "https://example.com/dpa",
	}
	repo.Add(testProcessor)

	t.Run("finds existing processor", func(t *testing.T) {
		p, err := repo.GetBySlug(ctx, "test-processor")
		if err != nil {
			t.Fatalf("GetBySlug() error = %v", err)
		}
		if p == nil {
			t.Fatal("GetBySlug() returned nil for existing processor")
		}
		if p.Name != "Test Processor" {
			t.Errorf("GetBySlug() Name = %v, want Test Processor", p.Name)
		}
		if p.Category != "testing" {
			t.Errorf("GetBySlug() Category = %v, want testing", p.Category)
		}
	})

	t.Run("returns nil for non-existent processor", func(t *testing.T) {
		p, err := repo.GetBySlug(ctx, "non-existent")
		if err != nil {
			t.Fatalf("GetBySlug() error = %v", err)
		}
		if p != nil {
			t.Error("GetBySlug() should return nil for non-existent processor")
		}
	})
}

func TestInMemoryProcessorRepository_GetByName(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryProcessorRepository()

	repo.Add(ProcessorProfileData{
		Name:     "Stripe Inc",
		Slug:     "stripe",
		Category: "payment",
	})

	t.Run("case insensitive match", func(t *testing.T) {
		testCases := []string{"Stripe Inc", "stripe inc", "STRIPE INC", "StRiPe InC"}
		for _, tc := range testCases {
			p, err := repo.GetByName(ctx, tc)
			if err != nil {
				t.Fatalf("GetByName(%q) error = %v", tc, err)
			}
			if p == nil {
				t.Errorf("GetByName(%q) returned nil", tc)
			}
		}
	})

	t.Run("returns nil for partial match", func(t *testing.T) {
		p, err := repo.GetByName(ctx, "Stripe")
		if err != nil {
			t.Fatalf("GetByName() error = %v", err)
		}
		if p != nil {
			t.Error("GetByName() should not match partial names")
		}
	})
}

func TestInMemoryProcessorRepository_GetByCategory(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryProcessorRepository()

	repo.Add(ProcessorProfileData{Name: "Stripe", Slug: "stripe", Category: "payment"})
	repo.Add(ProcessorProfileData{Name: "PayPal", Slug: "paypal", Category: "payment"})
	repo.Add(ProcessorProfileData{Name: "HubSpot", Slug: "hubspot", Category: "crm"})

	t.Run("returns all processors in category", func(t *testing.T) {
		procs, err := repo.GetByCategory(ctx, "payment")
		if err != nil {
			t.Fatalf("GetByCategory() error = %v", err)
		}
		if len(procs) != 2 {
			t.Errorf("GetByCategory() returned %d processors, want 2", len(procs))
		}
		for _, p := range procs {
			if p.Category != "payment" {
				t.Errorf("GetByCategory() returned processor with wrong category: %v", p.Category)
			}
		}
	})

	t.Run("returns empty slice for non-existent category", func(t *testing.T) {
		procs, err := repo.GetByCategory(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("GetByCategory() error = %v", err)
		}
		if len(procs) != 0 {
			t.Errorf("GetByCategory() returned %d processors for non-existent category, want 0", len(procs))
		}
	})
}

func TestInMemoryProcessorRepository_Search(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryProcessorRepository()

	repo.Add(ProcessorProfileData{Name: "Stripe", Slug: "stripe", Category: "payment"})
	repo.Add(ProcessorProfileData{Name: "Stripe Atlas", Slug: "stripe-atlas", Category: "incorporation"})
	repo.Add(ProcessorProfileData{Name: "PayPal", Slug: "paypal", Category: "payment"})
	repo.Add(ProcessorProfileData{Name: "AWS", Slug: "aws", Category: "cloud"})

	t.Run("search by name", func(t *testing.T) {
		procs, err := repo.Search(ctx, "stripe", 10)
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}
		if len(procs) < 2 {
			t.Errorf("Search() returned %d processors, want at least 2", len(procs))
		}
		for _, p := range procs {
			if !containsSubstring(p.Name, "Stripe") && !containsSubstring(p.Slug, "stripe") {
				t.Errorf("Search() returned processor not matching 'stripe': %v", p.Name)
			}
		}
	})

	t.Run("search by category", func(t *testing.T) {
		procs, err := repo.Search(ctx, "payment", 10)
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}
		if len(procs) < 2 {
			t.Errorf("Search() returned %d processors, want at least 2", len(procs))
		}
	})

	t.Run("search respects limit", func(t *testing.T) {
		procs, err := repo.Search(ctx, "stripe", 1)
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}
		if len(procs) > 1 {
			t.Errorf("Search() returned %d processors, want at most 1", len(procs))
		}
	})

	t.Run("search with no results", func(t *testing.T) {
		procs, err := repo.Search(ctx, "nonexistent", 10)
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}
		if len(procs) != 0 {
			t.Errorf("Search() returned %d processors for non-matching query, want 0", len(procs))
		}
	})
}

func TestInMemoryProcessorRepository_List(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryProcessorRepository()

	for i := 0; i < 10; i++ {
		repo.Add(ProcessorProfileData{
			Name: string(rune('A'+i)) + " Processor",
			Slug: string(rune('a'+i)) + "-processor",
		})
	}

	t.Run("list with pagination", func(t *testing.T) {
		// First page
		page1, err := repo.List(ctx, 0, 3)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(page1) != 3 {
			t.Errorf("List() returned %d processors, want 3", len(page1))
		}

		// Second page
		page2, err := repo.List(ctx, 3, 3)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(page2) != 3 {
			t.Errorf("List() returned %d processors, want 3", len(page2))
		}

		// Pages should have different processors
		for _, p1 := range page1 {
			for _, p2 := range page2 {
				if p1.Slug == p2.Slug {
					t.Error("List() pagination returned overlapping results")
				}
			}
		}
	})

	t.Run("list with offset beyond data", func(t *testing.T) {
		procs, err := repo.List(ctx, 100, 10)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(procs) != 0 {
			t.Errorf("List() returned %d processors for offset beyond data, want 0", len(procs))
		}
	})
}

func TestInMemoryProcessorRepository_Count(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryProcessorRepository()

	t.Run("empty repository", func(t *testing.T) {
		count, err := repo.Count(ctx)
		if err != nil {
			t.Fatalf("Count() error = %v", err)
		}
		if count != 0 {
			t.Errorf("Count() = %d, want 0", count)
		}
	})

	t.Run("after adding processors", func(t *testing.T) {
		repo.Add(ProcessorProfileData{Slug: "a"})
		repo.Add(ProcessorProfileData{Slug: "b"})
		repo.Add(ProcessorProfileData{Slug: "c"})

		count, err := repo.Count(ctx)
		if err != nil {
			t.Fatalf("Count() error = %v", err)
		}
		if count != 3 {
			t.Errorf("Count() = %d, want 3", count)
		}
	})
}

func TestInMemoryProcessorRepository_SeedCommonProcessors(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryProcessorRepository()
	repo.SeedCommonProcessors()

	t.Run("seeds common processors", func(t *testing.T) {
		count, err := repo.Count(ctx)
		if err != nil {
			t.Fatalf("Count() error = %v", err)
		}
		if count < 5 {
			t.Errorf("SeedCommonProcessors() seeded only %d processors, want at least 5", count)
		}
	})

	t.Run("includes Stripe", func(t *testing.T) {
		p, err := repo.GetBySlug(ctx, "stripe")
		if err != nil {
			t.Fatalf("GetBySlug() error = %v", err)
		}
		if p == nil {
			t.Error("SeedCommonProcessors() should include Stripe")
		}
		if p != nil && p.Category != "payment" {
			t.Errorf("Stripe category = %v, want payment", p.Category)
		}
		if p != nil && p.DPAURL == "" {
			t.Error("Stripe should have DPA URL")
		}
	})

	t.Run("includes HubSpot", func(t *testing.T) {
		p, err := repo.GetBySlug(ctx, "hubspot")
		if err != nil {
			t.Fatalf("GetBySlug() error = %v", err)
		}
		if p == nil {
			t.Error("SeedCommonProcessors() should include HubSpot")
		}
		if p != nil && p.Category != "crm" {
			t.Errorf("HubSpot category = %v, want crm", p.Category)
		}
	})

	t.Run("includes AWS", func(t *testing.T) {
		p, err := repo.GetBySlug(ctx, "aws")
		if err != nil {
			t.Fatalf("GetBySlug() error = %v", err)
		}
		if p == nil {
			t.Error("SeedCommonProcessors() should include AWS")
		}
		if p != nil && p.Category != "cloud_infrastructure" {
			t.Errorf("AWS category = %v, want cloud_infrastructure", p.Category)
		}
	})

	t.Run("processors have required fields", func(t *testing.T) {
		procs, err := repo.List(ctx, 0, 100)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		for _, p := range procs {
			if p.Name == "" {
				t.Errorf("Processor %s has empty name", p.Slug)
			}
			if p.Slug == "" {
				t.Errorf("Processor %s has empty slug", p.Name)
			}
			if p.Category == "" {
				t.Errorf("Processor %s has empty category", p.Name)
			}
			if p.Headquarters == "" {
				t.Errorf("Processor %s has empty headquarters", p.Name)
			}
			if p.TransferMechanism == "" {
				t.Errorf("Processor %s has empty transfer mechanism", p.Name)
			}
		}
	})
}

func TestInMemoryProcessorRepository_Close(t *testing.T) {
	repo := NewInMemoryProcessorRepository()
	err := repo.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestProcessorProfileDataFields(t *testing.T) {
	p := ProcessorProfileData{
		Name:              "Test Processor",
		Slug:              "test-processor",
		Category:          "testing",
		Headquarters:      "US",
		DataCategories:    []string{"email", "name", "phone"},
		ProcessingPurposes: []string{"marketing", "analytics"},
		DataLocations:     []string{"us", "eu"},
		TransferMechanism: "dpf",
		DPAStatus:         "in_place",
		DPAURL:            "https://example.com/dpa",
	}

	t.Run("all fields are set", func(t *testing.T) {
		if p.Name != "Test Processor" {
			t.Errorf("Name = %v, want Test Processor", p.Name)
		}
		if p.Slug != "test-processor" {
			t.Errorf("Slug = %v, want test-processor", p.Slug)
		}
		if p.Category != "testing" {
			t.Errorf("Category = %v, want testing", p.Category)
		}
		if p.Headquarters != "US" {
			t.Errorf("Headquarters = %v, want US", p.Headquarters)
		}
		if len(p.DataCategories) != 3 {
			t.Errorf("DataCategories length = %v, want 3", len(p.DataCategories))
		}
		if len(p.ProcessingPurposes) != 2 {
			t.Errorf("ProcessingPurposes length = %v, want 2", len(p.ProcessingPurposes))
		}
		if len(p.DataLocations) != 2 {
			t.Errorf("DataLocations length = %v, want 2", len(p.DataLocations))
		}
		if p.TransferMechanism != "dpf" {
			t.Errorf("TransferMechanism = %v, want dpf", p.TransferMechanism)
		}
		if p.DPAStatus != "in_place" {
			t.Errorf("DPAStatus = %v, want in_place", p.DPAStatus)
		}
		if p.DPAURL != "https://example.com/dpa" {
			t.Errorf("DPAURL = %v, want https://example.com/dpa", p.DPAURL)
		}
	})
}
