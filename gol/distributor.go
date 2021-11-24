package gol

import (
	"strconv"
	"sync"
	"time"
	"uk.ac.bris.cs/gameoflife/util"
)

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
	keys	   <-chan rune
}

// Board stores one game of life board and its width/height
type Board struct {
	cells [][]uint8
	width int
	height int
}

// Game stores the state of the boards, events and details about the ongoing game
type Game struct {
	current *Board // the current board
	advanced *Board // the current board after one turn
	width int
	height int
	completedTurns int
	mutex sync.Mutex
	events chan<- Event
}

// createBoard creates a board struct given a width and height
// Note we create the columns first, so we need to do cells[y][x]
func createBoard(width int, height int) *Board {
	cells := make([][]uint8, height)
	for x := range cells {
		cells[x] = make([]uint8, width)
	}
	return &Board{cells: cells, width: width, height: height}
}

// createGame creates an instance of Game
func createGame(width int, height int, c distributorChannels) *Game {
	current := createBoard(width, height)
	current.PopulateBoard(c) // set the cells of the current board to those from the input
	advanced := createBoard(width, height)
	return &Game{current: current, advanced: advanced, width: width, height: height,completedTurns: 0, events: c.events}
}

// PopulateBoard sets the board values to those from the input
func (board *Board) PopulateBoard(c distributorChannels) {
	for j:=0; j<board.height; j++ {
		for i:=0; i<board.width; i++ {
			value := <- c.ioInput
			board.Set(i, j, value)
			if value == 255 { // when first loading the board, send the event for all cells that are alive
				c.events <- CellFlipped{Cell: util.Cell{X: i, Y: j}}
			}
		}
	}
}

// Get sets the value of a cell
func (board *Board) Get (x int, y int) uint8 {
	return board.cells[y][x]
}

// Set sets the value of a cell
func (board *Board) Set (x int, y int, val uint8) {
	board.cells[y][x] = val
}

// Alive checks if a cell is alive, accounting for wrap around if necessary
func (board *Board) Alive(x int, y int, wrap bool) bool {
	if wrap {
		x = (x + board.width) % board.width // need to add the w and h for these as Go modulus doesn't like negatives
		y = (y + board.height) % board.height
	}
	return board.cells[y][x] == 255
}

// Neighbours checks all cells within 1 cell, then checks if each of these are alive to get the returned neighbour count
func (board *Board) Neighbours(x int, y int) int {
	aliveNeighbours := 0
	for i:=-1; i<=1; i++ {
		for j:=-1; j<=1; j++ {
			if i == 0 && j == 0 { // ensure we aren't counting the cell itself
				continue
			}
			if board.Alive(x+j, y+i, true) { // note: we are sorta repeating this unnecessarily for each cell?
				aliveNeighbours++
			}
		}
	}
	return aliveNeighbours
}

// AdvanceCell updates the value for a specific cell after a turn
// TODO: could probably clean this function up a bit, maybe only check Alive when we need to?
func (game *Game) AdvanceCell(x int, y int) {
	aliveNeighbours := game.current.Neighbours(x, y)
	var newCellValue uint8
	if game.current.Alive(x,y, false) { // if the cell is alive
		if aliveNeighbours < 2 || aliveNeighbours > 3 {
			newCellValue = 0 // dies
			game.events <- CellFlipped{CompletedTurns: game.completedTurns, Cell: util.Cell{X: x, Y: y}}
		} else {
			newCellValue = 255 // stays the same
		}
	} else { // if the cell is dead
		if aliveNeighbours == 3 {
			newCellValue = 255 // becomes alive
			game.events <- CellFlipped{CompletedTurns: game.completedTurns, Cell: util.Cell{X: x, Y: y}}
		} else {
			newCellValue = 0 // stays the same
		}
	}
	game.advanced.Set(x, y, newCellValue)
}

// AdvanceSection advances the board one turn only between the specified x and y values assigned to the worker
func (game *Game) AdvanceSection(startX int, endX int, startY int, endY int) {
	for j:=startY; j<endY; j++ {
		for i:=startX; i<endX; i++ {
			game.AdvanceCell(i, j)
		}
	}
}

// SpawnAdvanceWorker updates game.advanced based on game.current, from startY up to endY
func (game *Game) SpawnAdvanceWorker(wg *sync.WaitGroup, startX int, endX int, startY int, endY int) {
	defer wg.Done()
	game.AdvanceSection(startX, endX, startY, endY)
}

// Advance splits the board into horizontal slices. Each worker works on one section to advance the whole board one turn
func (game *Game) Advance(wg *sync.WaitGroup, workers int, width int, height int) {
	for i:=0; i<workers; i++ {
		startX := 0
		endX := width
		startY := i * height/workers
		var endY int
		if i == workers-1 { // make the last worker take the remaining space
			endY = height
		} else {
			endY = (i + 1) * height/workers
		}
		wg.Add(1)
		go game.SpawnAdvanceWorker(wg, startX, endX, startY, endY)
	}
}

// MonitorAliveCellCount gets the number of alive cells every 2sec and submits the event
// TODO: make concurrent?
func (game *Game) MonitorAliveCellCount(stopGame chan struct{}) {
	ticker := time.NewTicker(2 * time.Second) // every 2 seconds
	for {
		select {
		case <-ticker.C: // 2 seconds has passed
			game.mutex.Lock()
			count := 0
			for j:=0; j < game.height; j++ { // count number of alive cells
				for i:=0; i<game.width; i++ {
					if game.current.Alive(i,j,false) {
						count++
					}
				}
			}
			game.events <- AliveCellsCount{game.completedTurns, count}
			game.mutex.Unlock()
		case <-stopGame: // check if game is over
			ticker.Stop()
			return
		}
	}
}

// AliveCells returns a list of Cells that are alive at the end of the game
func (board *Board) AliveCells() []util.Cell {
	var aliveCells []util.Cell
	for j:=0; j<board.height; j++ {
		for i:=0; i<board.width; i++ {
			if board.Alive(i, j, false) {
				aliveCells = append(aliveCells, util.Cell{X: i, Y: j})
			}
		}
	}
	return aliveCells
}

// WriteImage outputs the final state of the board as a PGM image
// TODO: make concurrent?
func (game *Game) WriteImage(p Params, c distributorChannels) {
	c.ioCommand <- ioOutput
	filename := strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.ImageHeight) + "x" + strconv.Itoa(game.completedTurns)
	c.ioFilename <- filename
	game.mutex.Lock()
	for j := 0; j < p.ImageHeight; j++ {
		for i := 0; i < p.ImageWidth; i++{
			c.ioOutput <- game.current.Get(i, j)
		}
	}
	game.events <- ImageOutputComplete{game.completedTurns,filename}
	game.mutex.Unlock()
}

// MonitorKeyPresses follows the rules when certain keys are pressed
func (game *Game) MonitorKeyPresses(p Params, c distributorChannels, keyGameOver chan bool){
	for {
		key := <- c.keys
		switch key {
		case 's' :
			game.WriteImage(p,c)
		case 'q' :
			//game.WriteImage(p,c)
			keyGameOver <- true
			return
		}
	}
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels) {
	// make the filename and pass it through channel
	var filename string
	filename = strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.ImageHeight)
	c.ioCommand <- ioInput // start read the image
	c.ioFilename <- filename // pass the filename of the image

	game := createGame(p.ImageWidth, p.ImageHeight, c)

	stopGame := make(chan struct{})
	keyGameOver := make(chan bool)
	go game.MonitorAliveCellCount(stopGame)
	go game.MonitorKeyPresses(p, c, keyGameOver)

	workers := p.Threads
	var wg sync.WaitGroup
	out:
	for game.completedTurns < p.Turns { // execute the turns
		select {
		case <-keyGameOver:
			break out
		default:
		}
		game.Advance(&wg, workers, p.ImageWidth, p.ImageHeight)
		wg.Wait()

		game.mutex.Lock()
		// swap the boards since the old advanced is current, and we will update all cells of the new advanced anyway
		game.current, game.advanced = game.advanced, game.current
		game.completedTurns++
		game.mutex.Unlock()

		game.events <- TurnComplete{game.completedTurns}
	}

	close(stopGame)

	game.WriteImage(p, c)

	aliveCells := game.current.AliveCells()
	game.events <- FinalTurnComplete{game.completedTurns,aliveCells}

	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	game.events <- StateChange{game.completedTurns, Quitting}
	
	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(game.events)
}
