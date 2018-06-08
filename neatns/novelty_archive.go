package neatns

import (
	"github.com/yaricom/goNEAT/neat/genetics"
	"sort"
	"errors"
)

// The maximal allowed size for fittest items list
const fittestAllowedSize = 5

const archiveSeedAmount = 1

// The novelty archive contains all of the novel items we have encountered thus far.
// Using a novelty metric we can determine how novel a new item is compared to everything
// currently in the novelty set
type NoveltyArchive struct {
	// the all the novel items we have found so far
	NovelItems             []*NoveltyItem
	// the all novel items with fittest organisms associated found so far
	FittestItems           NoveltyItemsByFitness

	// the current generation
	Generation             int

	// the measure of novelty
	noveltyMetric          NoveltyMetric

	// the novel items added during current generation
	itemsAddedInGeneration int
	// the current generation index
	generationIndex        int

	// the minimum threshold for a "novel item"
	noveltyThreshold       float64
	// the minimal possible value of novelty threshold
	noveltyFloor           float64

	// the counter to keep track of how many gens since we've added to the archive
	timeOut                int

	// the parameter for how many neighbors to look at for N-nearest neighbor distance novelty
	neighbors              int
}

// Creates new instance of novelty archive
func NewNoveltyArchive(threshold float64, metric NoveltyMetric) *NoveltyArchive {
	arch := NoveltyArchive{
		NovelItems:make([]*NoveltyItem, 0),
		FittestItems:make([]*NoveltyItem, 0),
		noveltyMetric:metric,
		neighbors:KNNNoveltyScore,
		noveltyFloor:0.25,
		noveltyThreshold:threshold,
		generationIndex:archiveSeedAmount,

	}
	return &arch
}

// evaluate the novelty of a single individual organism within population
func (a *NoveltyArchive) EvaluateIndividual(org *genetics.Organism, pop *genetics.Population, onlyFitness bool) {
	item := org.Data.Value.(NoveltyItem)
	var result float64
	if onlyFitness {
		// assign organism fitness according to average novelty within archive and population
		result = a.noveltyAvgKnn(&item, -1, pop)
		org.Fitness = result
	} else {
		// consider adding a point to archive based on dist to nearest neighbor
		result = a.noveltyAvgKnn(&item, 1, nil)
		if result > a.noveltyThreshold || len(a.NovelItems) < archiveSeedAmount {
			a.addNoveltyItem(&item)
			item.Age = 1.0
		}
	}

	// store found values to the item
	item.Novelty = result
	item.Generation = a.Generation

	org.Data.Value = item
}

// add novelty item to archive
func (a *NoveltyArchive) addNoveltyItem(i *NoveltyItem) {
	i.added = true
	i.Generation = a.Generation
	a.NovelItems = append(a.NovelItems, i)
	a.itemsAddedInGeneration++
}

// to maintain list of fittest organisms so far
func (a *NoveltyArchive) updateFittestWithOrganism(org *genetics.Organism) error {
	if org.Data == nil {
		return errors.New("Organism with no Data provided")
	}

	if len(a.FittestItems) < fittestAllowedSize {
		// store organism's novelty item into fittest
		item := org.Data.Value.(NoveltyItem)
		a.FittestItems = append(a.FittestItems, &item)

		// sort to have most fit first
		sort.Sort(sort.Reverse(a.FittestItems))
	} else {
		last_item := a.FittestItems[len(a.FittestItems) - 1]
		org_item := org.Data.Value.(NoveltyItem)
		if org_item.Fitness > last_item.Fitness {
			// store organism's novelty item into fittest
			a.FittestItems = append(a.FittestItems, &org_item)

			// sort to have most fit first
			sort.Sort(sort.Reverse(a.FittestItems))

			// remove less fit item
			items := make([]*NoveltyItem, fittestAllowedSize)
			copy(items, a.FittestItems)
			a.FittestItems = items
		}
	}
	return nil
}

// to adjust dynamic novelty threshold depending on how many have been added to archive recently
func (a *NoveltyArchive) adjustArchiveSettings() {
	if a.itemsAddedInGeneration == 0 {
		a.timeOut++
	} else {
		a.timeOut = 0
	}

	// if no individuals have been added for 10 generations lower the threshold
	if a.timeOut == 10 {
		a.noveltyThreshold *= 0.95
		if a.noveltyThreshold < a.noveltyFloor {
			a.noveltyThreshold = a.noveltyFloor
		}
		a.timeOut = 0
	}

	// if more than four individuals added this generation raise threshold
	if a.itemsAddedInGeneration >= 4 {
		a.noveltyThreshold *= 1.2
	}

	a.itemsAddedInGeneration = 0
	a.generationIndex = len(a.NovelItems)
}

// the steady-state end of generation call
func (a *NoveltyArchive) endOfGeneration() {
	a.Generation++

	a.adjustArchiveSettings()
}

// the K nearest neighbor novelty score calculation for given item within provided population if any
func (a *NoveltyArchive) noveltyAvgKnn(item *NoveltyItem, neigh int, pop *genetics.Population) float64 {
	var novelties ItemsDistances
	if pop != nil {
		novelties = a.mapNoveltyInPopulation(item, pop)
	} else {
		novelties = a.mapNovelty(item)
	}

	// sort by distance - minimal first
	sort.Sort(novelties)

	density, sum, weight := 0.0, 0.0, 0.0
	length := len(novelties)

	// if neighbors size not set - use value from archive parameters
	if neigh == -1 {
		neigh = a.neighbors
	}

	if length >= archiveSeedAmount {
		length = neigh
		if len(novelties) < length {
			length = len(novelties)
		}
		i := 0
		for weight < float64(neigh) && i < len(novelties) {
			sum += novelties[i].distance
			weight += 1.0
			i++
		}

		// find average
		if weight > 0 {
			density = sum / weight
		}
	}

	return density
}

// map the novelty metric across the archive against provided item
func (a *NoveltyArchive) mapNovelty(item *NoveltyItem) ItemsDistances {
	novelties := make([]ItemsDistance, len(a.NovelItems))
	for i := 0; i < len(a.NovelItems); i++ {
		novelties[i] = ItemsDistance{
			distance:a.noveltyMetric(a.NovelItems[i], item),
			from:a.NovelItems[i],
			to:item,
		}
	}
	return ItemsDistances(novelties)
}

// map the novelty metric across the archive and the current population
func (a *NoveltyArchive) mapNoveltyInPopulation(item *NoveltyItem, pop *genetics.Population) ItemsDistances {
	novelties := make([]ItemsDistance, len(a.NovelItems) + len(pop.Organisms))
	n_index := 0
	for i := 0; i < len(a.NovelItems); i++ {
		novelties[n_index] = ItemsDistance{
			distance:a.noveltyMetric(a.NovelItems[i], item),
			from:a.NovelItems[i],
			to:item,
		}
		n_index++
	}

	for i := 0; i < len(pop.Organisms); i++ {
		org_item := pop.Organisms[i].Data.Value.(NoveltyItem)
		novelties[n_index] = ItemsDistance{
			distance:a.noveltyMetric(&org_item, item),
			from:&org_item,
			to:item,
		}
		n_index++
	}
	return ItemsDistances(novelties)
}